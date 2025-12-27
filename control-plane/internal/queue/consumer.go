
/**Deliveries are not acked here.

Your LeaseJob gRPC handler should:

try to lease in DB

Ack on success (or if message is malformed / not claimable)

Nack(requeue=true) for transient errors

That keeps “at least once” delivery without losing jobs.
*/

package queue

import (
	"context"
	"fmt"

	amqp "github.com/rabbitmq/amqp091-go"
)

type Consumer struct {
	deliveries chan amqp.Delivery
	errors	 chan error	
	ch *amqp.Channel
	queueName string
}

// Delveries is the channel of incoming deliveries(is the channel your runnergrpc/server.go LeaseJob method reads from)
func (c *Consumer) Deliveries() <-chan amqp.Delivery {
	return c.deliveries
}

// Errors is the channel of errors occurred in the consumer(fatal consumer errors (connection drop, channel close, etc.))
func (c *Consumer) Errors() <-chan error {
	return c.errors
}

func (c *Consumer) Close() error {
	if c.ch != nil {
		return c.ch.Close()
	}
	return nil
}

// StartJobConsumer starts a background consumer that pushes deliveries to a buffered channel.
// - prefetch controls how many unacked messages RabbitMQ will send at once.
// - buffer is the size of the Go channel buffer for deliveries.
func StartJobConsumer(ctx context.Context, amqpConn *amqp.Connection, queueName string, prefetch int, buffer int) (*Consumer, error) {
	ch, err := amqpConn.Channel()
	if err != nil {
		return nil, fmt.Errorf("failed to open channel: %w", err)
	}

	// Ensure the queue exists (match your existing settings)
	_, err = ch.QueueDeclare(
		queueName,
		true,  // durable
		false, // autoDelete
		false, // exclusive
		false, // noWait
		nil,   // args
	)
	if err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("failed to declare queue: %w", err)
	}

	// QoS: limit unacked in-flight messages for this consumer
	if prefetch <= 0 {
		prefetch = 10
	}

	if err := ch.Qos(prefetch, 0, false); err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("failed to set QoS: %w", err)
	}

	msgs, err := ch.Consume(
		queueName,
		"",    // consumer
		false, // autoAck
		false, // exclusive
		false, // noLocal
		false, // noWait
		nil,   // args
	)
	if err != nil {
		_ = ch.Close()
		return nil, fmt.Errorf("failed to start consuming: %w", err)
	}

	consumer := &Consumer{
		deliveries: make(chan amqp.Delivery, buffer),
		errors:     make(chan error, 1),
		ch:         ch,
		queueName:  queueName,
	}

	notifyClose := make(chan *amqp.Error)
	ch.NotifyClose(notifyClose)

	// Forward messages to the buffered channel so your gRPC server can select/timeout
	go func() {
		defer close(consumer.deliveries)
		defer close(consumer.errors)
		for {
			select {
			case <-ctx.Done():
				// context cancelled -> stop consumer
				return

			case amqpErr := <-notifyClose:
				if amqpErr != nil {
					consumer.errors <- fmt.Errorf("AMQP channel closed: %w", amqpErr)
				}
					return
				
					case d, ok := <-msgs:
					if !ok {
						// Consume channel closed (connection drop, channel closed, etc.)
						consumer.errors <- fmt.Errorf("RabbitMQ deliveries channel closed")
						return
					}
					consumer.deliveries <- d
			}
		}
	}()

	return consumer, nil
}