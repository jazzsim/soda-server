package main

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

var forever chan struct{}

type Video struct {
	Url      string `json:"url"`
	Filename string `json:"Filename"`
}

func failOnError(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s", msg, err)
	}
}

func main() {
	ch, q := connectRabbitMQ()

	for i := 0; i < 3; i++ {
		go thumbnailGeneration(ch, q.Name)
	}

	<-forever
}

func connectRabbitMQ() (*amqp.Channel, amqp.Queue) {
	// Connect to RabbitMQ server
	RABBITMQ_URL := os.Getenv("RABBITMQ_URL")
	conn, err := amqp.Dial(RABBITMQ_URL)
	failOnError(err, "Failed to connect to RabbitMQ")

	// Create a channel
	ch, err := conn.Channel()
	failOnError(err, "Failed to open a channel")

	q, err := ch.QueueDeclare(
		"thumbnail", // name
		false,       // durable
		false,       // delete when unused
		false,       // exclusive
		false,       // no-wait
		nil,         // arguments
	)
	failOnError(err, "Failed to declare a queue")

	return ch, q
}

func thumbnailGeneration(ch *amqp.Channel, queueName string) {
	// Example consumer
	msgs, err := ch.Consume(
		queueName, // queue
		"",        // consumer
		false,     // auto-ack
		false,     // exclusive
		false,     // no-local
		false,     // no-wait
		nil,       // args
	)
	failOnError(err, "Failed to register a consumer")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for d := range msgs {
		var video Video

		err := json.Unmarshal([]byte(d.Body), &video)
		if err != nil {
			log.Fatal(err)
		}

		// Generate thumbnail
		imgUrl := executeFFmpeg(video.Url, video.Filename)

		err = ch.PublishWithContext(ctx,
			"",        // exchange
			d.ReplyTo, // routing key
			false,     // mandatory
			false,     // immediate
			amqp.Publishing{
				ContentType:   "text/plain",
				CorrelationId: d.CorrelationId,
				Body:          []byte(imgUrl),
			})
		failOnError(err, "Failed to publish a message")

		d.Ack(false)
	}
}
