# Configuring torrc for Different Conjure Registration Methods

Connecting with Conjure requires a registration step. The Tor Conjure client
pluggable transport currently allows for 3 different methods of registration.

```
Bidirectional
AMP Cache
DNS
```

The dns registration method further allows for additional types (`DNS over
HTTPS`, `DNS over UDP`, and `DNS over TLS` as outlined in the `dnstt
protocol`(https://www.bamsoftware.com/software/dnstt/protocol.html) however,
our implementation selects from the DnsRegMethod identified in the dnsConf that
is in `"github.com/refraction-networking/conjure/pkg/client/assets"`.

Each registration method requires a different torrc configuration.

### Bidirectional Registration
This is the default registration method that the torrc file is setup to use. The transport can be altered to use one of:
one of 3 transport methods:
```
dtls
prefix
min
```

### AMP Cache Registration
Several changes to the torrc file must be made to make use of AMP cache
registration with the existing conjure station.

In the Bridge line, set the `registrar` flag to `ampcache` and the `ampcache` flag to the AMP cache URL (i.e., https://cdn.ampproject.org/). Finally set `url` to the conjure station's AMP cache address: `https://amp.regraction.network`.

Example Bridge line using AMP Cache Registration
```
 Bridge conjure 143.110.214.222:80 50B99540A96C5E9F9F7704BAAE11DF01564711F4 url=https://amp.refraction.network registrar=ampcache ampcache=https://cdn.ampproject.org/ fronts=www.google.com transport=prefix
```

### DNS Registration

Only one change to the torrc file must be made to make use of dns
registration with the existing conjure station.

In the Bridge line, simply set `registrar` to `dns`.

Example Bridge line using DNS Registration
```
Bridge conjure 143.110.214.222:80 50B99540A96C5E9F9F7704BAAE11DF01564711F4 registrar=dns url=https://registration.refraction.network fronts=cdn.zk.mk,www.cdn77.com transport=min
```

Note that this will work with any of the three currently supported transports,
but since `prefix` and `dtls` are larger, they may take slightly longer to
successfully connect.
