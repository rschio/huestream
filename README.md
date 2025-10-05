# huestream
[![Go Reference](https://pkg.go.dev/badge/github.com/rschio/huestream.svg)](https://pkg.go.dev/github.com/rschio/huestream)

This package implements the [Philips Hue Entertainment API](https://developers.meethue.com/develop/hue-entertainment/hue-entertainment-api/).
You can use it to change the lights Philips Hue in a fast to way, for example to sync with a music or a video.

## Example
```go
package huestream_test

import (
	"context"
	"image/color"
	"log"
	"math/rand/v2"
	"time"

	"github.com/amimof/huego"
	"github.com/rschio/huestream"
)

// WARNING: this example makes use of fast changing light effects conditions
// and it may trigger previously undetected epileptic symptoms or seizures
// in persons who have no history of prior seizures or epilepsy.
func Example() {
	// Run this only the first time and store the creds in an ENV var.
	host, username, clientKey, err := genClientCreds()
	if err != nil {
		log.Fatal(err)
	}

	// If you don't have an entertainment area yet, create it using the
	// Philips Hue App:
	// Settings > Entertainment areas > +.
	//
	// Use this command to get the ID of your first entertainment area,
	// if you have more than one and want to choose, adapt the command:
	//
	// curl -s -k \
	//    -H 'hue-application-key: <username>' \
	//    https://<host>/clip/v2/resource/entertainment_configuration | jq '.data.[0].id'
	//
	// Yes, I'm too lazy to write this function.
	areaID := ""

	// Create a context with timeout so the stream will finish in 5s.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start a stream in the selected Entertainment area.
	stream, err := huestream.Start(ctx, host, username, clientKey, areaID)
	if err != nil {
		log.Fatal(err)
	}
	defer stream.Close()

	log.Println("Connected")

	// From Hue Docs:
	// "The streaming makes use of UDP, which can result in that certain messages
	// get lost, that is why it is important to continuously stream messages, even
	// when it would mean repeating the same messages or light values, typically a
	// streaming rate of 50-60Hz is used."
	//
	// 50 Hz = 1 message each 20 ms.
	sendRate := time.Tick(time.Second / 50)

	// From Hue Docs:
	// "The bridge sends maximum at 25 Hz messages over ZigBee.
	// Thus, the (fastest) effect rate should be 2 – 3 times slower
	// than this 25 Hz, i.e < 12.5 Hz."
	//
	// 10 Hz = 1 message each 100 ms.
	changeColorRate := time.Tick(time.Second / 10)

	c0, c1 := randColor(), randColor()
	for {
		select {
		case <-ctx.Done():
			return

		// Log the errors.
		case err := <-stream.Error:
			log.Println(err)

		case <-changeColorRate:
			c0, c1 = randColor(), randColor()

		case <-sendRate:
			// Here we are sending two colors because my Entertainment Area has 2 lights.
			// The slice index represents the Channel ID (the light).
			// If you have 5 lights in your area, send a slice of 5 colors.
			stream.Send <- []color.Color{c0, c1}
		}
	}

}

func genClientCreds() (host, username, clientKey string, err error) {
	bridge, err := huego.Discover()
	if err != nil {
		return "", "", "", err
	}
	host = bridge.Host

	// Press the Bridge link button.
	user, err := bridge.CreateUserWithClientKey("my entertainment app")
	if err != nil {
		return "", "", "", err
	}

	return host, user.Username, user.ClientKey, nil
}

func randColor() color.Color {
	rnd := func() uint8 { return uint8(rand.IntN(256)) }
	return color.RGBA{R: rnd(), G: rnd(), B: rnd()}
}
```
