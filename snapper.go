package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"

	"time"

	"github.com/gorilla/mux"
	"github.com/mattn/go-scan"
	"github.com/sfreiberg/gotwilio"
	"github.com/spf13/viper"
	"golang.org/x/net/context"
	"golang.org/x/oauth2"
)

const (
	endpoint = "https://api.imgur.com/3/image"
)

type ImgurConfig struct {
	Album        string
	clientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string
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

	defer response.Body.Close()
	buf := bytes.NewBuffer(make([]byte, 0, response.ContentLength))
	_, err := buf.ReadFrom(response.Body)
	if err != nil {
		log.Fatal(err)
	}
	snapshot_body <- buf.Bytes()
	log.Println("passing snapshot")

}

func uploadToImgur(image_body []byte, title string, description string, imgurConfig *ImgurConfig, imgur_link chan string) {

	params := url.Values{
		"image":       {base64.StdEncoding.EncodeToString(image_body)},
		"album":       {imgurConfig.Album},
		"title":       {title},
		"description": {description},
	}

	var res *http.Response

	config := &oauth2.Config{
		ClientID:     imgurConfig.clientID,
		ClientSecret: imgurConfig.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://api.imgur.com/oauth2/authorize",
			TokenURL: "https://api.imgur.com/oauth2/token",
		},
	}

	token := new(oauth2.Token)
	token.AccessToken = imgurConfig.AccessToken
	token.RefreshToken = imgurConfig.RefreshToken
	token.Expiry = time.Now().Add(360)

	client := config.Client(context.Background(), token)
	//client, err := config.Client(context.Background(), token)

	res, err := client.PostForm(endpoint, params)
	if err != nil {
		log.Fatalln(os.Stderr, "post:", err)
		os.Exit(1)
	}

	if res.StatusCode != 200 {
		var message string
		err = scan.ScanJSON(res.Body, "data/error", &message)
		if err != nil {
			message = res.Status
		}
		log.Fatalln(os.Stderr, "post:", message)
		os.Exit(1)
	}
	defer res.Body.Close()

	var link string
	err = scan.ScanJSON(res.Body, "data/link", &link)
	if err != nil {
		log.Fatalln(os.Stderr, "post:", err)
		os.Exit(1)
	}
	imgur_link <- link

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

	cameras := viper.GetStringMap("Cameras")
	camera := cameras[camera_name].(map[string]interface{})

	t := time.Now()

	tz, err := time.LoadLocation("America/Chicago")

	date := t.In(tz).Format("3:04pm on Mon, Jan _2")

	title := camera["title"].(string)
	description := " 📷 " + title + " @ " + date

	//grab image from camera
	snapshot_url := viper.GetString("Cameras.entryway.url")

	snapshot_body := make(chan []byte)
	go grabSnapshot(snapshot_url, snapshot_body)

	body := <-snapshot_body
	log.Println("grabbed snapshot")

	// upload to imgur
	imgur_link := make(chan string)

	imgur := &ImgurConfig{Album: viper.GetString("imgur.album"),
		clientID:     viper.GetString("imgur.ClientID"),
		ClientSecret: viper.GetString("imgur.ClientSecret"),
		AccessToken:  viper.GetString("imgur.AccessToken"),
		RefreshToken: viper.GetString("imgur.RefreshToken"),
	}

	go uploadToImgur(body, title, description, imgur, imgur_link)

	link := <-imgur_link

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
