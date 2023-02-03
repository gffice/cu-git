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
	closed      chan struct{}
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
		close(c.closed)
		return c.conn.Close()
	}
	return nil
}

func (c *BufferedConn) SetConn(conn net.Conn) error {
	c.lock.Lock()
	c.closed = make(chan struct{})
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
		go c.checkForStaleness()
		// Do not reset buffer in case this connection fails
		//c.buffer.Reset()
	}
	c.conn = conn
	go c.checkForStaleness()
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

func (c *BufferedConn) checkForStaleness() {
	c.lastReceive = time.Now()
	for {
		lastReceive := c.lastReceive
		if time.Since(lastReceive) > ConjureStalenessTimeout {
			log.Printf("Connection to the conjure station has timed out. Closing stale connection")
			c.Close()
			return
		}
		select {
		case <-c.closed:
			return
		case <-time.After(time.Second):
		}

	}
}
