package main

import (
	"bytes"

	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"

    MQTT "github.com/eclipse/paho.mqtt.golang"

    "os/signal"
    "syscall"
    "strings"

	"os"

	"time"



	"github.com/sfreiberg/gotwilio"
	"github.com/spf13/viper"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awsutil"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
)

const (
	endpoint = "https://api.imgur.com/3/image"
)

type AWSConfig struct {
	bucket       string
	awsAccessKey string
	awsSecret    string
	awsRegion    string
}

type TwilioConfig struct {
	accountSid string
	authToken  string
	to         []string
	from       string
}

func grabSnapshot(url string, snapshot_body chan []byte) {
	// don't worry about errors
	log.Println("Grabbing snapshot")
	response, e := http.Get(url)
	if e != nil {
		log.Fatal(e)
	}
	log.Println(response.ContentLength)

	defer response.Body.Close()
	buf := bytes.NewBuffer(make([]byte, 0))
	_, err := buf.ReadFrom(response.Body)
	if err != nil {
		log.Fatal(err)
	}
	snapshot_body <- buf.Bytes()
	log.Println("passing snapshot")

}

func uploadToS3(image_body []byte, title string, cameraname string, awsConfig *AWSConfig, s3_object_link chan string) {
	token := ""
	creds := credentials.NewStaticCredentials(awsConfig.awsAccessKey, awsConfig.awsSecret, token)
	_, err := creds.Get()
	if err != nil {
		log.Printf("bad credentials: %s", err)
	}
	cfg := aws.NewConfig().WithRegion(awsConfig.awsRegion).WithCredentials(creds)
	svc := s3.New(session.New(), cfg)
	fileBytes := bytes.NewReader(image_body)

	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	filename := timestamp + ".jpg"
	log.Println(filename)

	path := "/" + cameraname + "/" + filename
	params := &s3.PutObjectInput{
		Bucket:      aws.String(awsConfig.bucket),
		Key:         aws.String(path),
		Body:        fileBytes,
		ContentType: aws.String("image/jpeg"),
	}
	resp, err := svc.PutObject(params)
	if err != nil {
		log.Printf("bad response: %s", err)
	}
	log.Printf("response %s", awsutil.StringValue(resp))

	req, _ := svc.GetObjectRequest(&s3.GetObjectInput{
		Bucket: aws.String(awsConfig.bucket),
		Key:    aws.String(path),
	})
	url, err := req.Presign(300 * time.Second)
	if err != nil {
		log.Printf("error %s", err)
	}

	s3_object_link <- url
}

func sendMMS(message string, link string, twilioConfig *TwilioConfig) {
	twilio := gotwilio.NewTwilioClient(twilioConfig.accountSid, twilioConfig.authToken)

	from := twilioConfig.from
	to := twilioConfig.to

	for _, number := range to {
		log.Println("Sending SMS: " + number)
		// element is the element from someSlice for where we are
		twilio.SendMMS(from, number, message, link, "", "")
	}
}

func takeSnapshot(camera_name string){
	log.Println(camera_name)
	log.Println("Handle the dudes")

	cameras := viper.GetStringMap("Cameras")

	camera := cameras[camera_name].(map[string]interface{})
	log.Println(camera["url"])

	t := time.Now()

	tz, err := time.LoadLocation("America/Chicago")
	if err != nil { // Handle errors reading the config file
		log.Fatalf("bad timezone: %s \n", err)
	}

	date := t.In(tz).Format("3:04pm on Mon, Jan _2")

	title := camera["title"].(string)
	description := title + " @ " + date

	//grab image from camera
	log.Println("Grab Image from camera")

	snapshot_url := camera["url"].(string)

	snapshot_body := make(chan []byte)
	go grabSnapshot(snapshot_url, snapshot_body)

	body := <-snapshot_body
	log.Println("grabbed snapshot")

	aws := &AWSConfig{bucket: viper.GetString("aws.bucket"),
		awsAccessKey: viper.GetString("aws.accessKeyId"),
		awsSecret:    viper.GetString("aws.secretAccessKey"),
		awsRegion:    viper.GetString("aws.region"),
	}
	s3_link := make(chan string)
	go uploadToS3(body, title, camera_name, aws, s3_link)
	link := <-s3_link

	// send notification
	log.Println("Send notification")

	notification_url := viper.GetString("notifications.url")
	form := url.Values{
		"image_url": {link},
		"message":   {description},
	}

	notifBody := bytes.NewBufferString(form.Encode())
	rsp, err := http.Post(notification_url, "application/x-www-form-urlencoded", notifBody)
	if err != nil {
		log.Fatal(err)
		log.Fatalf("Error connecting to notification server")

	}
	defer rsp.Body.Close()
}

// handlers



var snapshotHandler MQTT.MessageHandler = func(client MQTT.Client, msg MQTT.Message) {
    fmt.Printf("MSG: %s\n", msg.Payload())

	camera_name := strings.Split(msg.Topic(), "/")[2]
	go takeSnapshot(camera_name)
    fmt.Printf("MSG: %s\n", camera_name)
    text := fmt.Sprintf("this is result msg #%d!", 1)

    token := client.Publish("reedazawa/snapshot_result", 0, false, text)
    token.Wait()
}

func main() {

	//set up logging
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("[Snapper] ")
	log.Println("Starting up...")

	// handlers


	viper.SetConfigName("snapper-config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		log.Fatalf("Fatal error config file: %s \n", err)
	}

	broker := os.Getenv("BROKER")
	if broker == "" {
		broker = viper.GetString("mqtt.broker")
	}

	topic := os.Getenv("TOPIC")
	if topic == "" {
		topic = viper.GetString("mqtt.topic")
	}
	


    c := make(chan os.Signal, 1)
    signal.Notify(c, os.Interrupt, syscall.SIGTERM)

    opts := MQTT.NewClientOptions().AddBroker(broker)
    opts.SetClientID("mac-go")
    opts.SetDefaultPublishHandler(snapshotHandler)


    opts.OnConnect = func(c MQTT.Client) {
            if token := c.Subscribe(topic, 0, snapshotHandler); token.Wait() && token.Error() != nil {
                    panic(token.Error())
            }
    }
    client := MQTT.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
            panic(token.Error())
    } else {
            fmt.Printf("Connected to server\n")
    }
    <-c

}
