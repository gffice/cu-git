package conjure

import (
	"bytes"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

const ConjureStalenessTimeout = 5 * time.Second

type BufferedConn struct {
	conn     net.Conn
	buffer   bytes.Buffer
	lock     sync.Mutex
	rp       *io.PipeReader
	wp       *io.PipeWriter
	once     sync.Once
	received chan struct{}
}

func NewBufferedConn() *BufferedConn {

	buffConn := new(BufferedConn)
	buffConn.rp, buffConn.wp = io.Pipe()
	buffConn.received = make(chan struct{})
	return buffConn
}

func (c *BufferedConn) Read(b []byte) (int, error) {
	n, err := c.rp.Read(b)
	c.once.Do(func() {
		log.Printf("Received data, connection is not stale")
		close(c.received)
	})
	return n, err
}

func (c *BufferedConn) Write(b []byte) (int, error) {
	c.lock.Lock()
	defer c.lock.Unlock()

	if c.conn == nil {
		log.Printf("Buffering %d bytes to send later", len(b))
		c.buffer.Write(b)
	} else {
		c.conn.Write(b)
	}
	return len(b), nil
}

func (c *BufferedConn) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *BufferedConn) SetConn(reset chan struct{}, success chan struct{}, conn net.Conn) error {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.buffer.Len() > 0 {
		n, err := conn.Write(c.buffer.Bytes())
		if err != nil {
			return err
		}
		go func() {
			io.Copy(c.wp, conn)
		}()
		log.Printf("Flushed %d bytes from buffer", n)
		go c.checkForStaleness(reset, success)
	}
	c.conn = conn
	return nil
}

func (c *BufferedConn) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}
func (c *BufferedConn) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}
func (c *BufferedConn) SetDeadline(t time.Time) error {
	return c.conn.SetDeadline(t)
}
func (c *BufferedConn) SetReadDeadline(t time.Time) error {
	return c.conn.SetReadDeadline(t)
}
func (c *BufferedConn) SetWriteDeadline(t time.Time) error {
	return c.conn.SetWriteDeadline(t)
}

func (c *BufferedConn) checkForStaleness(reset chan struct{}, success chan struct{}) {
	select {
	case <-c.received:
		success <- struct{}{}
		c.buffer.Reset()
		return
	case <-time.After(ConjureStalenessTimeout):
		log.Printf("Connection to the conjure station has timed out. Reset stale connection")
		reset <- struct{}{}
	}

}
