module gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/conjure

go 1.17

require (
	git.torproject.org/pluggable-transports/snowflake.git/v2 v2.5.1
	github.com/pires/go-proxyproto v0.7.0
	github.com/refraction-networking/conjure v0.6.7
	github.com/refraction-networking/gotapdance v1.6.8
	gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/goptlib v1.5.0
	gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/snowflake/v2 v2.6.1
)

require (
	github.com/andybalholm/brotli v1.0.5 // indirect
	github.com/dchest/siphash v1.2.3 // indirect
	github.com/flynn/noise v1.0.0 // indirect
	github.com/gaukas/godicttls v0.0.4 // indirect
	github.com/golang/protobuf v1.5.3 // indirect
	github.com/klauspost/compress v1.16.7 // indirect
	github.com/libp2p/go-reuseport v0.3.0 // indirect
	github.com/pion/dtls/v2 v2.2.7 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/sctp v1.8.7 // indirect
	github.com/pion/stun v0.6.1 // indirect
	github.com/pion/transport/v2 v2.2.2-0.20230802201558-f2dffd80896b // indirect
	github.com/refraction-networking/ed25519 v0.1.2 // indirect
	github.com/refraction-networking/obfs4 v0.1.2 // indirect
	github.com/refraction-networking/utls v1.3.3 // indirect
	github.com/sergeyfrolov/bsbuffer v0.0.0-20180903213811-94e85abb8507 // indirect
	github.com/sirupsen/logrus v1.9.3 // indirect
	golang.org/x/crypto v0.12.0 // indirect
	golang.org/x/net v0.13.0 // indirect
	golang.org/x/sys v0.11.0 // indirect
	golang.org/x/text v0.12.0 // indirect
	google.golang.org/protobuf v1.31.0 // indirect
)

replace github.com/pion/dtls/v2 => github.com/mingyech/dtls/v2 v2.0.0
