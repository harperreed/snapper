package main

import (
	"bytes"

	"fmt"
	"log"
	"net/http"
	"strconv"

	"os"

	"time"

	"github.com/gorilla/mux"

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

// handlers

func snapshotHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	camera_name := vars["filename"]
	log.Println(camera_name)

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
	description := " 📷 " + title + " @ " + date

	//grab image from camera
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

	// send sms

	twilioConfig := &TwilioConfig{
		accountSid: viper.GetString("twilio.accountSid"),
		authToken:  viper.GetString("twilio.authToken"),
		to:         viper.GetStringSlice("twilio.to"),
		from:       viper.GetString("twilio.from"),
	}
	message := description

	go sendMMS(message, link, twilioConfig)
	log.Println(description)

	fmt.Fprintf(w, "Snapshot taken", r.URL.Path[1:])
}

func main() {

	//set up logging
	log.SetFlags(log.LstdFlags)
	log.SetPrefix("[Snapper] ")
	log.Println("Starting up...")

	// handlers
	r := mux.NewRouter()
	r.HandleFunc("/snapshot/{filename}.jpg", snapshotHandler)

	http.Handle("/", r)

	viper.SetConfigName("snapper-config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	err := viper.ReadInConfig() // Find and read the config file
	if err != nil {             // Handle errors reading the config file
		log.Fatalf("Fatal error config file: %s \n", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = viper.GetString("http.port")
	}

	addr := ":" + port

	//Listen on non-tls
	log.Println("Listening [" + addr + "]...")
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Println("Serving on " + addr + " failed")
		log.Fatal(err)
	}

}
