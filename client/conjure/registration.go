package conjure

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/refraction-networking/conjure/pkg/registrars/registration"
	transports "github.com/refraction-networking/conjure/pkg/transports/client"
	"github.com/refraction-networking/conjure/proto"
	"github.com/refraction-networking/gotapdance/tapdance"
	utls "github.com/refraction-networking/utls"

	utlsutil "gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/ptutil/utls"

	"gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/snowflake/v2/common/certs"
)

type ConjureConfig struct {
	RegisterURL   string // URL of the conjure bidirectional registration API endpoint
	Front         string
	BridgeAddress string // IP address of the Tor Conjure PT bridge
	UTLSClientID  string
	UTLSRemoveSNI bool
}

type Rendezvous struct {
	RegisterURL   string
	Front         string
	Transport     http.RoundTripper
	UTLSClientID  string
	UTLSRemoveSNI bool
}

func (r *Rendezvous) RoundTrip(req *http.Request) (*http.Response, error) {

	log.Println("Performing a Conjure registration with domain fronting...")
	log.Println("Conjure station URL: ", r.RegisterURL)
	log.Println("Domain front: ", r.Front)

	if r.Front != "" {
		req.Host = req.URL.Host
		req.URL.Host = r.Front
	}

	return r.Transport.RoundTrip(req)
}

// We make a copy of DefaultTransport because we want the default Dial
// and TLSHandshakeTimeout settings. But we want to disable the default
// ProxyFromEnvironment setting.
func createRegistrationTransport() http.RoundTripper {
	tlsConfig := &tls.Config{
		RootCAs: certs.GetRootCAs(),
	}
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	transport.Proxy = nil
	transport.ResponseHeaderTimeout = 15 * time.Second
	return transport
}

func Register(config *ConjureConfig) (net.Conn, error) {

	dialer := &tapdance.Dialer{
		// Use conjure to connect to phantom addresses, not vanilla tapdance
		DarkDecoy: true,
		// If true, the station sends PROXY header in the connection from the
		// station to the conjure bridge that includes the client's IP address
		UseProxyHeader: true,
		// For MVP, we don't (yet) support v6 phantoms
		V6Support: false,
		// Width is specific to the decoy routing registrat, at the moment we're using
		// only the bidirectional registrar
		Width: 0,
	}

	transport := createRegistrationTransport()
	if config.UTLSClientID != "" {
		utlsClienHelloID, err := utlsutil.NameToUTLSID(config.UTLSClientID)
		if err != nil {
			return nil, fmt.Errorf("unable to create ")
		}
		utlsConfig := &utls.Config{
			RootCAs: certs.GetRootCAs(),
		}

		transport = utlsutil.NewUTLSHTTPRoundTripperWithProxy(utlsClienHelloID, utlsConfig, transport, config.UTLSRemoveSNI, nil)

	}

	// APIRegistrarBidirectional expects an HTTP client for sending the registration request.
	// The http.RoundTripper associated with this client dictates the censorship-resistant
	// rendezvous method used to establish a connection with the registration server.
	// As of now, the deployed Conjure station supports direct HTTP connections and domain
	// fronted connections.
	client := &http.Client{
		Transport: &Rendezvous{
			RegisterURL:   config.RegisterURL,
			Front:         config.Front,
			Transport:     transport,
			UTLSClientID:  config.UTLSClientID,
			UTLSRemoveSNI: config.UTLSRemoveSNI,
		},
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
	regConfig := &registration.Config{
		Target: config.RegisterURL + "/api/register-bidirectional", //Note: this goes in the HTTP request
		// TODO: reach out and ask what a reasonable value to set this to is
		Delay:         time.Second,
		MaxRetries:    0,
		Bidirectional: true,
		HTTPClient:    client,
	}

	registrar, err := registration.NewAPIRegistrar(regConfig)
	if err != nil {
		return nil, err
	}

	dialer.DarkDecoyRegistrar = registrar

	// There are currently three available transports:
	//   1) min
	//   2) prefix
	//   3) obfs4
	params := &proto.GenericTransportParams{}
	dialer.TransportConfig, err = transports.NewWithParams("min", params)
	if err != nil {
		return nil, err
	}

	log.Printf("Using the registration API at %s", regConfig.Target)
	// Make a connection to the bridge through the phantom
	// This will register the client, obtaining a phantom address and connect
	// to that phantom address all in one go
	phantomConn, err := dialer.DialContext(context.Background(), "tcp", config.BridgeAddress)
	if err != nil {
		return nil, err
	}

	log.Println("Successfully connected to phantom proxy!")

	return phantomConn, nil
}
