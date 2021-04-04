package listener

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/Limgmk/leedns/dns"
	R "github.com/Limgmk/leedns/resolver"
	D "github.com/miekg/dns"
)

type Listener struct {
	ServiceType string
	Addr        string
	CertFile    string
	KeyFile     string
	HttpPath    string
}

type handler struct {
	r *R.Resolver
	l *Listener
}

func (h handler) ServeDNS(w dns.ResponseWriter, q *dns.Query) {
	var qStr string

	if len(q.Msg.Question) > 0 {
		qq := q.Msg.Question[0]
		qStr = fmt.Sprintf("[%s %s %s]", qq.Name, D.ClassToString[qq.Qclass], D.TypeToString[qq.Qtype])
		log.Printf("%s at %s: %s from %s", h.l.ServiceType, h.l.Addr, qStr, q.Remote.Addr)
	}

	m, err := h.r.Exchange(q.Msg)
	if err != nil {
		log.Println(err.Error())
	}
	if m == nil {
		log.Printf("%s: No result from upstreams and hosts file", qStr)
		m = new(D.Msg)
	}

	m.SetReply(q.Msg)

	err = w.WriteMsg(m)
	if err != nil {
		log.Println(err.Error())
	}
}

func Start(listener []*Listener, resolver *R.Resolver) {

	for _, l := range listener {
		h := new(handler)
		h.r = resolver
		h.l = l
		var err error
		switch l.ServiceType {
		case "udp", "tcp":
			err = dns.ListenAndServe(l.Addr, l.ServiceType, h)
		case "tls", "tcp-tls":
			err = dns.ListenAndServeTLS(l.Addr, l.CertFile, l.KeyFile, h)
		case "http":
			err = dns.ListenHTTPAndServe(l.Addr, l.HttpPath, h)
		case "https":
			err = dns.ListenHTTPAndServeTLS(l.Addr, l.HttpPath, l.CertFile, l.KeyFile, h)
		default:
			err = fmt.Errorf("error network type: %s", l.ServiceType)
		}
		if err != nil {
			log.Printf("Start %v DNS Server Error at %v: %v\n", l.ServiceType, l.Addr, err.Error())
		} else {
			log.Printf("Start %v DNS Service Listening at: %v\n", l.ServiceType, l.Addr)
		}
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	fmt.Printf("\nSignal [%s] received, stopping\n", <-sig)
}
