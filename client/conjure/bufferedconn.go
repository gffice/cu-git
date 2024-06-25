package conjure

import (
	"bytes"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

type BufferedConn struct {
	conn   net.Conn
	buffer bytes.Buffer
	lock   sync.Mutex
	rp     *io.PipeReader
	wp     *io.PipeWriter
}

func NewBufferedConn() *BufferedConn {

	buffConn := new(BufferedConn)
	buffConn.rp, buffConn.wp = io.Pipe()
	return buffConn
}

func (c *BufferedConn) Read(b []byte) (int, error) {
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
		return c.conn.Close()
	}
	return nil
}

func (c *BufferedConn) SetConn(conn net.Conn) error {
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
		c.buffer.Reset()
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
