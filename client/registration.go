package main

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"time"

	proto "github.com/refraction-networking/gotapdance/protobuf"
	"github.com/refraction-networking/gotapdance/tapdance"
)

type Rendezvous struct {
	registerURL string
	front       string
	transport   *http.Transport
}

func (r *Rendezvous) RoundTrip(req *http.Request) (*http.Response, error) {

	log.Println("Performing a Conjure registration with domain fronting...")
	log.Println("Conjure station URL: ", r.registerURL)
	log.Println("Domain front: ", r.front)

	if r.front != "" {
		req.Host = req.URL.Host
		req.URL.Host = r.front
	}

	return r.transport.RoundTrip(req)
}

func proxy(socks net.Conn, phantom net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		if _, err := io.Copy(socks, phantom); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			log.Printf("Error copying SOCKS to phantom %v", err)
		}
		socks.Close()
		phantom.Close()
		wg.Done()
	}()
	go func() {
		if _, err := io.Copy(phantom, socks); err != nil && !errors.Is(err, io.ErrClosedPipe) {
			log.Printf("Error copying phantom to SOCKS %v", err)
		}
		socks.Close()
		phantom.Close()
		wg.Done()
	}()
	wg.Wait()
}

func handle(conn net.Conn, config *ConjureConfig) error {

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	dialer := &tapdance.Dialer{
		// Use conjure to connect to phantom addresses, not vanilla tapdance
		DarkDecoy: true,
		//TODO: what is this?
		SplitFlows: false,
		//TODO: what is this?
		// Apparently psiphon for android needs it but i'm not sure why yet
		TcpDialer: nil,
		// If true, the station sends PROXY header in the connection from the
		// station to the conjure bridge that includes the client's IP address
		UseProxyHeader: true,
		// For MVP, we don't (yet) support v6 phantoms
		V6Support: false,
		//TODO: double check this
		Width: 0,
	}

	// The registration step connects a client with a phantom IP address.
	// There are currently two options for registration:
	//   1) APIRegistrarBidirectional: this is a bidirectional registration process that allows a
	//        a client to submit a REST API request over HTTP for the phantom IP
	//   2) DecoyRegistrar: this is a unidirectional registration process used by the
	//        original TapDance protocol in which the client essentially tells the refraction
	//        station which phantom IP to use
	// For simplicity, we use the bidirectional registration process. Different censorship
	// resistant transport methods can be used to tunnel the HTTP requests, such as domain fronting
	registrar := tapdance.APIRegistrarBidirectional{
		Endpoint: config.registerURL + "/register-bidirectional", //Note: this goes in the HTTP request
		// TODO: reach out and ask what a reasonable value to set this to is
		ConnectionDelay: time.Second,
		MaxRetries:      0,
	}

	transport := http.DefaultTransport.(*http.Transport)
	transport.Proxy = nil

	// APIRegistrarBidirectional expects an HTTP client for sending the registration request.
	// The http.RoundTripper associated with this client dictates the censorship-resistant
	// rendezvous method used to establish a connection with the registration server.
	// As of now, the deployed Conjure station supports direct HTTP connections and domain
	// fronted connections.
	registrar.Client = &http.Client{
		Transport: &Rendezvous{
			registerURL: config.registerURL,
			front:       config.front,
			transport:   transport,
		},
	}

	dialer.DarkDecoyRegistrar = registrar

	// There are currently three available transports:
	//   1) min
	//   2) obfs4
	//   3) webrtc
	dialer.Transport = proto.TransportType_Min

	log.Printf("Using the registration API at %s", config.registerURL)
	// Make a connection to the bridge through the phantom
	// This will register the client, obtaining a phantom address and connect
	// to that phantom address all in one go
	phantomConn, err := dialer.DialContext(ctx, "tcp", "192.168.122.3:8888")
	if err != nil {
		return err
	}
	defer phantomConn.Close()

	log.Println("Successfully connected to phantom proxy!")

	proxy(conn, phantomConn)
	log.Println("Closed connection to phantom proxy")

	return nil
}
