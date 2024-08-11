package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"time"

	"github.com/gin-gonic/gin"
	"github.com/gocolly/colly"
	gocache "github.com/patrickmn/go-cache"
	amqp "github.com/rabbitmq/amqp091-go"
)

var conn *amqp.Connection

// Change this to false if you don't want to generate thumbnail
var generateThumbnail = true

var cache = gocache.New(5*time.Minute, 1*time.Hour)

type HttpServer struct {
	Url      string `json:"url"`
	Username string `json:"username"`
	Password string `json:"password"`
}

type Video struct {
	Url      string `json:"url"`
	Filename string `json:"filename"`
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

func main() {
	if generateThumbnail {
		// Connect to RabbitMQ server
		connectRabbitMQ()
	}

	ginMode := os.Getenv("ENV")
	gin.SetMode(ginMode)

	router := gin.Default()
	router.ForwardedByClientIP = true
	router.SetTrustedProxies([]string{"127.0.0.1"})
	router.Use(CORSMiddleware())
	router.POST("/api/scrape", scrape)
	router.POST("/api/thumbnail", getThumbnail)

	router.Run("0.0.0.0:8080")
}

func connectRabbitMQ() {
	// Connect to RabbitMQ
	RABBITMQ_URL := os.Getenv("RABBITMQ_URL")
	connection, err := amqp.Dial(RABBITMQ_URL)
	failOnError(err, "Failed to connect to RabbitMQ")

	conn = connection
}

func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func scrape(c *gin.Context) {
	server := HttpServer{}

	if err := c.ShouldBindJSON(&server); err != nil {
		// Handle error (e.g., invalid JSON format)
		c.JSON(http.StatusBadRequest, gin.H{"400 error": err.Error()})
		return
	}

	_, err := server.verifyUrl()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"500 error": err.Error()})
		return
	}

	// Instantiate default collector
	collector := colly.NewCollector()

	credentialsNeeded := server.Username != "" && server.Password != ""
	if credentialsNeeded {
		auth := "Basic " + base64.StdEncoding.EncodeToString([]byte(server.Username+":"+server.Password))
		collector.OnRequest(func(r *colly.Request) {
			r.Headers.Set("Authorization", auth)
		})
	}

	contents := Contents{Folders: []string{}, Files: []Files{}}
	contents.retrieveContents(collector)

	collector.OnScraped(func(r *colly.Response) {
		fmt.Println("Finished", r.Request.URL)
		c.JSON(http.StatusOK, contents)
	})

	collector.Visit(server.Url)
}

func (s *HttpServer) verifyUrl() (*url.URL, error) {
	// Parse the URL
	parseUrl, err := url.Parse(s.Url)
	if err != nil {
		fmt.Println("Error parsing URL:", err)
		return nil, err
	}
	return parseUrl, nil
}

func getThumbnail(c *gin.Context) {
	if !generateThumbnail {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Thumbnail generation is disabled"})
		return
	}
	res, err := consumeThumbnailRequest(c)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get thumbnail"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"thumbnail": res})
}

func consumeThumbnailRequest(c *gin.Context) (res string, err error) {
	// get data from post request
	video := Video{}

	if err := c.ShouldBindJSON(&video); err != nil {
		// Handle error (e.g., invalid JSON format)
		c.JSON(http.StatusBadRequest, gin.H{"400 error": err.Error()})
		return "", err
	}

	ext := strings.ToLower(filepath.Ext(video.Url))
	if ext == ".ogg" {
		return "", errors.New("unsupported format")
	}

	// check if image is cached
	if cacheUrl, ok := cache.Get(video.Filename); ok {
		return cacheUrl.(string), nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")
	defer ch.Close()

	q, err := ch.QueueDeclare(
		"",    // name
		false, // durable
		true,  // delete when unused
		false, // exclusive
		false, // no-wait
		nil,   // arguments
	)
	failOnError(err, "Failed to declare a queue")

	if ch == nil || ch.IsClosed() {
		cha, err := conn.Channel()
		failOnError(err, "Failed to open a channel")
		ch = cha
		defer ch.Close()
	}

	body, err := json.Marshal(video)
	if err != nil {
		log.Fatalf("Failed to encode message body: %v", err)
	}

	err = ch.PublishWithContext(ctx,
		"",          // exchange
		"thumbnail", // routing key
		false,       // mandatory
		false,       // immediate
		amqp.Publishing{
			ContentType:   "application/json",
			CorrelationId: video.Filename,
			ReplyTo:       q.Name,
			Body:          body,
		})

	failOnError(err, "Failed to publish a message")


	// thumbnail completed consumer
	msgs, err := ch.Consume(
		q.Name, // queue
		"",     // consumer
		true,   // auto-ack
		false,  // exclusive
		false,  // no-local
		false,  // no-wait
		nil,    // args
	)
	failOnError(err, "Failed to register a consumer")

	// Wait for messages
	d, ok := <-msgs

	if !ok {
		log.Println("channel closed")
		return "", nil
	}

	if d.CorrelationId == video.Filename {
		res = string(d.Body)
		err = d.Ack(false)
		failOnError(err, "Failed to ack a message")
	}

	// cache image
	cache.Set(video.Filename, res, 23*time.Hour)

	return res, err
}