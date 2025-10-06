package huestream

import (
	"cmp"
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image/color"
	"net"
	"net/http"
	"strings"
	"sync"

	"github.com/pion/dtls/v3"
)

// Start initiates a new stream in the given area. Use the stream to change the
// colors of the lamps.
func Start(ctx context.Context, host, username, clientKey, areaID string) (*Stream, error) {
	c := newClient(host, username, clientKey)
	return c.initStream(ctx, areaID)
}

// Stream manages the Hue Entertainment Stream of an Entertainment Area.
type Stream struct {
	once   sync.Once
	conn   *dtls.Conn
	client *client
	areaID string
}

// Close closes the connection, stops the stream and release the resources.
func (s *Stream) Close() error {
	var err error

	s.once.Do(func() {
		err = cmp.Or(
			s.client.stopStream(context.Background(), s.areaID),
			s.conn.Close(),
		)
	})

	return err
}

// Send a command to change the color of the lamps.
// The int value is the Channel ID (lamp ID).
func (s *Stream) Send(idColors map[int]color.Color) error {
	msg := message{areaID: s.areaID, idColors: idColors}
	b, err := msg.MarshalBinary()
	if err != nil {
		return err
	}
	_, err = s.conn.Write(b)
	return err
}

// client is used to initiate a Stream.
type client struct {
	http *http.Client

	host       string // The Hue Bridge IP.
	username   string // The username returned when creating a Hue user.
	clientKey  string // The clientKey returned when creating a Hue user.
	streamPort int    // The streamPort is always 2100.
}

// newClient creates a new client used to start a Hue Entertainment Stream.
//
// See the Example to know how to get the host, username and clientKey.
func newClient(host, username, clientKey string) *client {
	transport := *http.DefaultTransport.(*http.Transport)
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	c := &http.Client{
		Transport: &transport,
	}

	return &client{
		http:       c,
		host:       host,
		username:   username,
		clientKey:  clientKey,
		streamPort: 2100,
	}
}

// initStream initiates a stream in the given area.
// Only one stream session can take place at a time.
func (c *client) initStream(ctx context.Context, areaID string) (*Stream, error) {
	if err := c.startStream(ctx, areaID); err != nil {
		return nil, err
	}
	conn, err := c.handshakeUDP(ctx)
	if err != nil {
		return nil, err
	}

	stream := &Stream{
		conn:   conn,
		areaID: areaID,
		client: c,
	}

	return stream, nil
}

func (c *client) setAuthHeader(req *http.Request) {
	req.Header.Set("hue-application-key", c.username)
}

func (c *client) baseURL() string {
	return fmt.Sprintf("https://%s/clip/v2/resource/entertainment_configuration", c.host)
}

func (c *client) streamAction(ctx context.Context, areaID, action string) error {
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

func (c *client) startStream(ctx context.Context, areaID string) error {
	return c.streamAction(ctx, areaID, "start")
}

func (c *client) stopStream(ctx context.Context, areaID string) error {
	return c.streamAction(ctx, areaID, "stop")
}

func (c *client) handshakeUDP(ctx context.Context) (*dtls.Conn, error) {
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

type message struct {
	areaID   string
	idColors map[int]color.Color
}

func (m message) MarshalBinary() ([]byte, error) {
	if len(m.idColors) > 20 {
		return nil, fmt.Errorf("maximum number of channels is 20, got %d", len(m.idColors))
	}

	// https://developers.meethue.com/develop/hue-entertainment/hue-entertainment-api/#StreamCaption
	// MaxSize = 192 bytes.
	var buf []byte
	buf = append(buf, "HueStream"...) // Protocol name.
	buf = append(buf, 0x2, 0x0)       // Version 2.0.
	buf = append(buf, 0x0)            // Sequence ID - ignored.
	buf = append(buf, 0x0, 0x0)       // Reserved 2 bytes.
	buf = append(buf, 0x0)            // ColorSpace = RGB.
	buf = append(buf, 0x0)            // Reserved 1 byte.
	buf = append(buf, m.areaID...)    // EntertainmentConfID.

	for channelID, color := range m.idColors {
		// An int can overflow, but it would be a callers error,
		// the max channelID is 20, even a uint8 would not solve the issue.
		buf = append(buf, byte(channelID))

		// RGBA returns alpha-premultiplied colors, so just discard the alpha.
		r, g, b, _ := color.RGBA()
		buf = binary.BigEndian.AppendUint16(buf, uint16(r))
		buf = binary.BigEndian.AppendUint16(buf, uint16(g))
		buf = binary.BigEndian.AppendUint16(buf, uint16(b))
	}

	return buf, nil
}
