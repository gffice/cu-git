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
	conn   net.Conn
	buffer bytes.Buffer
	lock   sync.Mutex
	rp     *io.PipeReader
	wp     *io.PipeWriter

	lastReceive time.Time
}

func NewBufferedConn() *BufferedConn {

	buffConn := new(BufferedConn)
	buffConn.rp, buffConn.wp = io.Pipe()
	return buffConn
}

func (c *BufferedConn) Read(b []byte) (int, error) {
	c.lastReceive = time.Now()
	return c.rp.Read(b)
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
		c.conn.Close()
	}
	return nil
}

func (c *BufferedConn) SetConn(reset chan struct{}, conn net.Conn) error {
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
		go c.checkForStaleness(reset)
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

func (c *BufferedConn) checkForStaleness(reset chan struct{}) {
	c.lastReceive = time.Now()
	for {
		lastReceive := c.lastReceive
		if time.Since(lastReceive) > ConjureStalenessTimeout {
			log.Printf("Connection to the conjure station has timed out. Reset stale connection")
			c.buffer.Reset()
			close(reset)
			return
		}
		select {
		case <-reset:
			return
		case <-time.After(time.Second):
		}

	}
}
