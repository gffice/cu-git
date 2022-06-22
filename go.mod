module git.torproject.org/pluggable-transports/conjure

go 1.17

require git.torproject.org/pluggable-transports/goptlib.git v1.2.0

require github.com/pires/go-proxyproto v0.6.2

require (
	git.torproject.org/pluggable-transports/snowflake.git v1.1.0
	github.com/golang/protobuf v1.5.2
	github.com/refraction-networking/gotapdance v1.2.0
	github.com/refraction-networking/utls v1.1.0 // indirect
)

replace github.com/refraction-networking/gotapdance => github.com/cohosh/gotapdance v0.0.0-20220602200913-1c737ba57600
