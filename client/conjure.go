// Client implementation of the Conjure PT adapted for Tor
package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/refraction-networking/conjure/pkg/client/assets"
	"github.com/refraction-networking/gotapdance/tapdance"

	"gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/conjure/client/conjure"
	pt "gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/goptlib"
	"gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/ptutil/safelog"
)

const RetryInterval = 10 * time.Second

// Get SOCKS arguments and populate config
func getSOCKSArgs(conn *pt.SocksConn, config *conjure.ConjureConfig) {
	// Check to see if our command line options are overriden by SOCKS options
	if arg, ok := conn.Req.Args.Get("registrar"); ok {
		config.Registrar = arg
	}
	if arg, ok := conn.Req.Args.Get("ampcache"); ok {
		config.AMPCacheURL = arg
	}
	if arg, ok := conn.Req.Args.Get("url"); ok {
		config.RegisterURL = arg
	}
	if arg, ok := conn.Req.Args.Get("fronts"); ok {
		if arg != "" {
			config.Fronts = strings.Split(strings.TrimSpace(arg), ",")
		}
	} else if arg, ok := conn.Req.Args.Get("front"); ok {
		config.Fronts = strings.Split(strings.TrimSpace(arg), ",")
	}
	if arg, ok := conn.Req.Args.Get("utls-nosni"); ok {
		switch strings.ToLower(arg) {
		case "true":
			fallthrough
		case "yes":
			config.UTLSRemoveSNI = true
		}
	}
	if arg, ok := conn.Req.Args.Get("utls-imitate"); ok {
		config.UTLSClientID = arg
	}
	if arg, ok := conn.Req.Args.Get("transport"); ok {
		config.Transport = arg
	}
	if arg, ok := conn.Req.Args.Get("stun"); ok {
		config.STUNAddr = arg
	}
}

// handle the SOCKS conn
func handler(conn *pt.SocksConn, config *conjure.ConjureConfig) error {

	defer conn.Close()

	shutdown := make(chan struct{})

	bridgeAddr, err := net.ResolveTCPAddr("tcp", conn.Req.Target)
	if err != nil {
		conn.Reject()
		return err
	}
	config.BridgeAddress = conn.Req.Target
	log.Printf("Attempting to connect to bridge at %s", conn.Req.Target)

	// optimistically grant all incoming SOCKS connections and start buffering data
	err = conn.Grant(bridgeAddr)
	if err != nil {
		return err
	}
	buffConn := conjure.NewBufferedConn()
	reset := make(chan struct{})
	success := make(chan struct{})

	go func() {
		for {
			phantomConn, err := conjure.Register(config)
			if err == nil {
				log.Printf("Connected to bridge at %s", conn.Req.Target)
				if err := buffConn.SetConn(reset, success, phantomConn); err != nil {
					log.Printf("Error setting internal conn: %s", err.Error())
				} else {
					log.Printf("Registration successful, checking for staleness. . .")
				}
			} else {
				log.Printf("Error registering with station: %s", err.Error())
				log.Printf("This may be due to high load, trying again.")
				pt.Log(pt.LogSeverityNotice,
					"retrying conjure registration, station is under high load.")
			}
			select {
			case <-time.After(RetryInterval):
				continue
			case <-reset:
				continue
			case <-success:
				return
			case <-shutdown:
				log.Println("Registration loop stopped")
				return
			}
		}
	}()

	proxy(conn, buffConn)
	log.Println("Closed connection to phantom proxy")
	close(shutdown)
	return nil
}

func acceptLoop(ln *pt.SocksListener, config *conjure.ConjureConfig) error {
	defer ln.Close()

	for {
		conn, err := ln.AcceptSocks()
		if err != nil {
			if e, ok := err.(net.Error); ok && e.Temporary() {
				pt.Log(pt.LogSeverityError, "accept error: "+err.Error())
				continue
			}
			return err
		}
		log.Printf("SOCKS accepted: %v", conn.Req)
		getSOCKSArgs(conn, config)
		go func() {
			err := handler(conn, config)
			if err != nil {
				log.Println(err)
			}
		}()
	}
}

func proxy(socks io.ReadWriteCloser, phantom io.ReadWriteCloser) {
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

func main() {
	assetDir := flag.String("assets", "", "asset directory for conjure configs")
	logFilename := flag.String("log", "", "name of the log file")
	logToStateDir := flag.Bool("log-to-state-dir", false,
		"resolve the log file relative to tor's pt state dir")
	unsafeLogging := flag.Bool("unsafe-logging", false, "prevent logs from being scrubbed")
	frontDomainsCommas := flag.String("fronts", "", "comma-separated list of front domains")
	registrar := flag.String("registrar", "bdapi", "One of bdapi, ampcache, dns")
	ampCacheURL := flag.String("ampcache", "", "URL of AMP cache to use as a proxy for signaling, must set registrar to ampcache")
	registerURL := flag.String("registerURL", "", "URL of the conjure registration station")
	uTLSClientHelloID := flag.String("utls-imitate", "", "type of TLS client to imitate with utls")
	uTLSRemoveSNI := flag.Bool("utls-nosni", false, "remove SNI from client hello(ignored if uTLS is not used)")
	defaultTransport := flag.String("transport", "min", "default transport to connect to phantom proxies")
	stunAddr := flag.String("stun", "stun.antisip.com:3478", "STUN server address needed for IP retrieval, use with ampCacheURL specified")

	flag.Parse()

	stateDir, err := pt.MakeStateDir()
	if err != nil {
		log.Fatal(err)
	}

	// Set up logging
	var logFile io.Writer
	logFile = io.Discard
	if *logFilename != "" {
		if *logToStateDir {
			*logFilename = filepath.Join(stateDir, *logFilename)
		}
		f, err := os.OpenFile(*logFilename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		logFile = f
	}
	if !*unsafeLogging {
		logFile = &safelog.LogScrubber{Output: logFile}
	}
	var frontDomains []string
	if *frontDomainsCommas != "" {
		frontDomains = strings.Split(strings.TrimSpace(*frontDomainsCommas), ",")
	}

	log.SetFlags(log.LstdFlags | log.LUTC)
	log.SetOutput(logFile)

	if *assetDir == "" {
		*assetDir = stateDir + "conjure"
		err := os.Mkdir(*assetDir, 0755)
		if err != nil && !os.IsExist(err) {
			log.Fatal(err)
		}
	}
	assets.AssetsSetDir(*assetDir)
	tapdance.SetLoggerOutput(logFile)
	tapdance.Logger().Warnf("Redirecting log to file")

	// Configure Conjure
	config := &conjure.ConjureConfig{
		Registrar:     *registrar,
		RegisterURL:   *registerURL,
		Fronts:        frontDomains,
		AMPCacheURL:   *ampCacheURL,
		UTLSClientID:  *uTLSClientHelloID,
		UTLSRemoveSNI: *uTLSRemoveSNI,
		Transport:     *defaultTransport,
		STUNAddr:      *stunAddr,
	}

	// Tor client-side transport setup
	var ln *pt.SocksListener
	ptInfo, err := pt.ClientSetup(nil)
	if err != nil {
		log.Fatal(err)
	}
	if ptInfo.ProxyURL != nil {
		pt.ProxyError("proxy is not supported")
		os.Exit(1)
	}

	for _, methodName := range ptInfo.MethodNames {
		switch methodName {
		case "conjure":
			ln, err = pt.ListenSocks("tcp", "127.0.0.1:0")
			if err != nil {
				pt.CmethodError(methodName, err.Error())
				break
			}
			log.Printf("Started SOCKS listener at %v", ln.Addr())
			go acceptLoop(ln, config)
			pt.Cmethod(methodName, ln.Version(), ln.Addr())
		default:
			pt.CmethodError(methodName, "no such method")
		}
	}
	pt.CmethodsDone()

	// shutdown handling
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGTERM)

	// https://gitweb.torproject.org/torspec.git/tree/pt-spec.txt#n203
	if os.Getenv("TOR_PT_EXIT_ON_STDIN_CLOSE") == "1" {
		go func() {
			if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
				log.Printf("Error copying os.Stdin to ioutil.Discard: %v", err)
			}
			log.Printf("Terminating because of stdin close")
			sigChan <- syscall.SIGTERM
		}()
	}

	<-sigChan
	log.Println("shutting down conjure")
	ln.Close()
}
