package huestream

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image/color"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/pion/dtls/v3"
)

func Start(ctx context.Context, host, username, clientKey, areaID string) (*Stream, error) {
	c := New(host, username, clientKey)
	return c.StartStream(ctx, areaID)
}

// Client is used to initiate a Stream.
type Client struct {
	http *http.Client

	host       string // The Hue Bridge IP.
	username   string // The username returned when creating a Hue user.
	clientKey  string // The clientKey returned when creating a Hue user.
	streamPort int    // The streamPort is always 2100.
}

// New creates a new client used to start a Hue Entertainment Stream.
//
// See the Example to know how to get the host, username and clientKey.
func New(host, username, clientKey string) *Client {
	transport := *http.DefaultTransport.(*http.Transport)
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	c := &http.Client{
		Transport: &transport,
	}

	return &Client{
		http:       c,
		host:       host,
		username:   username,
		clientKey:  clientKey,
		streamPort: 2100,
	}
}

// Stream manages the Hue Entertainment Stream of an Entertainment Area.
type Stream struct {
	// Send an []color.Color to the Send channel, each slice element represents
	// one channel (or one light), so if your Entertainment Area has 3 lights,
	// send a slice with 3 elements every time, the slice index is the channel ID.
	//
	// Don't close the Send channel directly, use Stream.Close().
	Send chan<- []color.Color

	// Error chan send errors in a buffered channel, new errors are discarded if
	// the buffer is full.
	Error <-chan error

	once sync.Once
	done chan struct{}

	conn   *dtls.Conn
	areaID string

	client *Client
}

// Close closes the connection, also closes the Send and Error channels.
func (s *Stream) Close() error {
	var err error

	s.once.Do(func() {
		close(s.Send)
		<-s.done
		// TODO: refactor stopStream.
		errStop := s.client.stopStream(context.Background(), s.areaID)
		err = s.conn.Close()
		err = cmp.Or(errStop, err)
	})

	return err
}

// StartStream initiates a stream in the given area.
//
// Only one stream session can take place at a time.
func (c *Client) StartStream(ctx context.Context, areaID string) (*Stream, error) {
	if err := c.startStream(ctx, areaID); err != nil {
		return nil, err
	}
	conn, err := c.handshakeUDP(ctx)
	if err != nil {
		return nil, err
	}

	colors := make(chan []color.Color)
	errs := make(chan error, 10)
	done := make(chan struct{})
	stream := &Stream{
		Send:   colors,
		Error:  errs,
		done:   done,
		conn:   conn,
		areaID: areaID,
		client: c,
	}

	go func() {
		for cs := range colors {
			_, err := encodeMsg(stream.conn, areaID, cs)
			if err != nil {
				select {
				case errs <- err:
				default:
				}
			}
		}
		close(errs)
		close(done)
	}()

	return stream, nil
}

func (c *Client) setAuthHeader(req *http.Request) {
	req.Header.Set("hue-application-key", c.username)
}

func (c *Client) baseURL() string {
	return fmt.Sprintf("https://%s/clip/v2/resource/entertainment_configuration", c.host)
}

func (c *Client) streamAction(ctx context.Context, areaID, action string) error {
	url := c.baseURL() + "/" + areaID
	data := strings.NewReader(fmt.Sprintf(`{"action":%q}`, action))
	req, err := http.NewRequestWithContext(ctx, "PUT", url, data)
	if err != nil {
		return err
	}
	c.setAuthHeader(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status code not OK, got %d", resp.StatusCode)
	}

	return nil
}

func (c *Client) startStream(ctx context.Context, areaID string) error {
	return c.streamAction(ctx, areaID, "start")
}

func (c *Client) stopStream(ctx context.Context, areaID string) error {
	return c.streamAction(ctx, areaID, "stop")
}

func (c *Client) handshakeUDP(ctx context.Context) (*dtls.Conn, error) {
	addr := &net.UDPAddr{IP: net.ParseIP(c.host), Port: c.streamPort}
	config := &dtls.Config{
		PSK: func(hint []byte) ([]byte, error) {
			return hex.DecodeString(c.clientKey)
		},
		PSKIdentityHint: []byte(c.username),
		CipherSuites:    []dtls.CipherSuiteID{dtls.TLS_PSK_WITH_AES_128_GCM_SHA256},
	}

	conn, err := dtls.Dial("udp", addr, config)
	if err != nil {
		return nil, fmt.Errorf("dial %v: %w", addr, err)
	}

	if err := conn.HandshakeContext(ctx); err != nil {
		return nil, fmt.Errorf("handshake: %w", err)
	}

	return conn, nil
}

func encodeMsg(w io.Writer, areaID string, cs []color.Color) (int, error) {
	if len(cs) > 20 {
		return 0, fmt.Errorf("maximum number of channels is 20, got %d", len(cs))
	}

	var buf []byte
	buf = append(buf, "HueStream"...) // Protocol name.
	buf = append(buf, 0x2, 0x0)       // Version 2.0.
	buf = append(buf, 0x0)            // Sequence ID - ignored.
	buf = append(buf, 0x0, 0x0)       // Reserved 2 bytes.
	buf = append(buf, 0x0)            // ColorSpace = RGB.
	buf = append(buf, 0x0)            // Reserved 1 byte.
	buf = append(buf, areaID...)      // EntertainmentConfID.

	for i, c := range cs {
		buf = append(buf, byte(i)) // Channel ID.

		// RGBA returns alpha-premultiplied colors, so just discard the alpha.
		r, g, b, _ := c.RGBA()
		buf = binary.BigEndian.AppendUint16(buf, uint16(r))
		buf = binary.BigEndian.AppendUint16(buf, uint16(g))
		buf = binary.BigEndian.AppendUint16(buf, uint16(b))
	}

	return w.Write(buf)
}
