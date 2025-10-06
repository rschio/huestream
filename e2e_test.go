//go:build e2e

package huestream_test

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"testing"
	"time"

	"github.com/rschio/huestream"
)

var (
	areaID     = mustEnv("HUESTREAM_AREA_ID")
	bridgeHost = mustEnv("HUESTREAM_BRIDGE_HOST")
	username   = mustEnv("HUESTREAM_USERNAME")
	clientKey  = mustEnv("HUESTREAM_CLIENT_KEY")
)

func TestNilMessage(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	stream, err := huestream.Start(ctx, bridgeHost, username, clientKey, areaID)
	if err != nil {
		t.Fatalf("hue.StartStream: %v", err)
	}
	defer stream.Close()

	if err := stream.Send(nil); err != nil {
		t.Fatal(err)
	}
}

func TestMoreThan20Channels(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	stream, err := huestream.Start(ctx, bridgeHost, username, clientKey, areaID)
	if err != nil {
		t.Fatalf("hue.StartStream: %v", err)
	}
	defer stream.Close()

	m := make(map[int]color.Color)
	for i := range 21 {
		m[i] = color.White
	}

	if err := stream.Send(m); err == nil {
		t.Error("should reject more than 20 channelIDs")
	}
}

func TestOnlyOneLamp(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	stream, err := huestream.Start(ctx, bridgeHost, username, clientKey, areaID)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()

	// 50 Hz = 1 message each 20 ms.
	sendRate := time.Tick(time.Second / 50)

	// 10 Hz = 10 each 1s.
	changeRate := time.Tick(time.Second / 10)

	var i int

	for {
		select {
		case <-ctx.Done():
			return
		case <-changeRate:
			i = (i + 1) % len(colors)
		case <-sendRate:
			cs := map[int]color.Color{
				1: colors[i][1],
			}
			if err := stream.Send(cs); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestE2E(t *testing.T) {
	// Call test to 2x in sequence to see if it's closing the connection correctly
	// and releasing the resources for a second connection.
	testE2E(t)
	testE2E(t)
}

func testE2E(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	stream, err := huestream.Start(ctx, bridgeHost, username, clientKey, areaID)
	if err != nil {
		t.Fatalf("hue.StartStream: %v", err)
	}
	defer func() {
		err := stream.Close()
		if err != nil {
			t.Errorf("stream.Close: %v", err)
		}
	}()

	// 50 Hz = 1 message each 20 ms.
	sendRate := time.Tick(time.Second / 50)

	// 10 Hz = 10 each 1s.
	changeRate := time.Tick(time.Second / 10)

	var i int

	for {
		select {
		case <-ctx.Done():
			return
		case <-changeRate:
			i = (i + 1) % len(colors)
		case <-sendRate:
			stream.Send(colors[i])
		}
	}
}

var colors = [...]map[int]color.Color{
	map[int]color.Color{
		0: color.RGBA{R: 87, G: 139, B: 45},
		1: color.RGBA{R: 163, G: 173, B: 193},
		2: color.RGBA{R: 115, G: 37, B: 178},
	},
	map[int]color.Color{
		0: color.RGBA{R: 236, G: 210, B: 224},
		1: color.RGBA{R: 98, G: 42, B: 29},
		2: color.RGBA{R: 185, G: 65, B: 73},
	},
	map[int]color.Color{
		0: color.RGBA{R: 178, G: 154, B: 78},
		1: color.RGBA{R: 252, G: 68, B: 165},
		2: color.RGBA{R: 243, G: 223, B: 137},
	},
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic(fmt.Sprintf("empty key %s", key))
	}
	return v
}
