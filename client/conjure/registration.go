package conjure

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"time"

	"github.com/refraction-networking/conjure/pkg/client/assets"
	"github.com/refraction-networking/conjure/pkg/registrars/registration"
	transports "github.com/refraction-networking/conjure/pkg/transports/client"
	"github.com/refraction-networking/conjure/proto"
	pb "github.com/refraction-networking/conjure/proto"
	"github.com/refraction-networking/gotapdance/tapdance"
	utls "github.com/refraction-networking/utls"

	utlsutil "gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/ptutil/utls"

	"gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/snowflake/v2/common/certs"
)

type ConjureConfig struct {
	Registrar     string
	RegisterURL   string // URL of the conjure bidirectional registration API endpoint
	Fronts        []string
	AMPCacheURL   string
	BridgeAddress string // IP address of the Tor Conjure PT bridge
	UTLSClientID  string
	UTLSRemoveSNI bool
	Transport     string
	STUNAddr      string
}

type Rendezvous struct {
	RegisterURL   string
	Fronts        []string
	Transport     http.RoundTripper
	UTLSClientID  string
	UTLSRemoveSNI bool
}

func (r *Rendezvous) RoundTrip(req *http.Request) (*http.Response, error) {

	log.Println("Performing a Conjure registration with domain fronting...")
	log.Println("Conjure station URL: ", r.RegisterURL)

	if len(r.Fronts) != 0 {
		// Do domain fronting. Replace the domain in the URL's with a randomly
		// selected front, and store the original domain the HTTP Host header.
		rand.Seed(time.Now().UnixNano())
		front := r.Fronts[rand.Intn(len(r.Fronts))]
		log.Println("Domain front: ", front)
		req.Host = req.URL.Host
		req.URL.Host = front
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

	var registrar tapdance.Registrar
	var err error

	// APIRegistrarBidirectional expects an HTTP client for sending the registration request.
	// The http.RoundTripper associated with this client dictates the censorship-resistant
	// rendezvous method used to establish a connection with the registration server.
	// As of now, the deployed Conjure station supports direct HTTP connections and domain
	// fronted connections.
	client := &http.Client{
		Transport: &Rendezvous{
			RegisterURL:   config.RegisterURL,
			Fronts:        config.Fronts,
			Transport:     transport,
			UTLSClientID:  config.UTLSClientID,
			UTLSRemoveSNI: config.UTLSRemoveSNI,
		},
	}

	// The registration step connects a client with a phantom IP address.
	// There are currently three options for registration:
	//   1) APIRegistrarBidirectional: this is a bidirectional registration process that allows
	//        a client to submit a REST API request over HTTP for the phantom IP
	//   2) AMPCacheRegistrarBidirectional: this is a bidirectional registration process that
	//       allows a client to use AMPCache as a proxy to submit a request to the registration
	//       server for the phantom IP
	//   3) DecoyRegistrar: this is a unidirectional registration process used by the
	//        original TapDance protocol in which the client essentially tells the refraction
	//        station which phantom IP to use
	// For simplicity, we implement support for the bidirectional
	// registration and ampcache registration processes. Different censorship resistant
	// transport methods can be used to tunnel the HTTP requests, such as domain fronting
	regConfig := &registration.Config{
		Bidirectional: true,
		HTTPClient:    client,
		STUNAddr:      config.STUNAddr,
	}
	switch config.Registrar {
	case "ampcache":
		if config.AMPCacheURL == "" {
			log.Println("AMP Cache registrar selected with no AMP cache URL")
			return nil, err
		}
		regConfig.Target = config.RegisterURL + "/amp/register-bidirectional" //Note: this goes in the HTTP request
		regConfig.AMPCacheURL = config.AMPCacheURL
		regConfig.MaxRetries = 0
		regConfig.HTTPClient = client
		log.Println("Register through AMP cache at:", regConfig.Target)
		registrar, err = registration.NewAMPCacheRegistrar(regConfig)
	case "dns":
		dnsConf := assets.Assets().GetDNSRegConf()
		pubkey := dnsConf.Pubkey
		if pubkey == nil {
			pubkey = assets.Assets().GetConjurePubkey()[:]
		}
		regConfig.Pubkey = dnsConf.Pubkey
		var method registration.DNSTransportMethodType
		switch *dnsConf.DnsRegMethod {
		case pb.DnsRegMethod_UDP:
			method = registration.UDP
		case pb.DnsRegMethod_DOT:
			regConfig.UTLSDistribution = *dnsConf.UtlsDistribution
			method = registration.DoT
		case pb.DnsRegMethod_DOH:
			regConfig.UTLSDistribution = *dnsConf.UtlsDistribution
			method = registration.DoH
		default:
			return nil, errors.New("unknown reg method in conf")
		}
		regConfig.DNSTransportMethod = method
		regConfig.Target = *dnsConf.Target
		regConfig.BaseDomain = *dnsConf.Domain
		regConfig.Pubkey = pubkey
		regConfig.MaxRetries = 3
		regConfig.STUNAddr = *dnsConf.StunServer
		log.Println("Register through DNS at:", regConfig.Target)
		registrar, err = registration.NewDNSRegistrar(regConfig)
	case "bdapi":
		fallthrough
	default:
		log.Println("Register through API with:", regConfig.Target)
		regConfig.Target = config.RegisterURL + "/api/register-bidirectional" //Note: this goes in the HTTP request
		regConfig.MaxRetries = 0
		regConfig.HTTPClient = client
		registrar, err = registration.NewAPIRegistrar(regConfig)
	}
	if err != nil {
		return nil, err
	}
	dialer.DarkDecoyRegistrar = registrar

	// There are currently three available transports:
	//   1) min
	//   2) prefix
	//   3) dtls
	var params any
	switch config.Transport {
	case "dtls":
		randomize := true
		unordered := false
		params = &proto.DTLSTransportParams{RandomizeDstPort: &randomize, Unordered: &unordered}
	case "prefix":
		randomize := true
		id := int32(-1)
		params = &proto.PrefixTransportParams{RandomizeDstPort: &randomize, PrefixId: &id}
	default:
		params = &proto.GenericTransportParams{}
		config.Transport = "min"
	}

	dialer.TransportConfig, err = transports.NewWithParams(config.Transport, params)
	if err != nil {
		return nil, err
	}

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
