package resolver

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	LEC "github.com/Limgmk/leedns/cache"
	"github.com/Limgmk/leedns/dns"
	D "github.com/miekg/dns"
	"github.com/robfig/cron/v3"
)

type queryStrategy func(m *D.Msg, r *Resolver) (msg *D.Msg, err error)

type Client struct {
	*ClientConfig
	currentWeight int
	c             dns.Client
	failedTimes   int
	lock          sync.RWMutex
}

type ClientConfig struct {
	URL    string
	Weight int
}

type Config struct {
	ClientsConfig []*ClientConfig
	Cache         bool
	Strategy      string
}

type Resolver struct {
	Hosts           Hosts
	StrategyFun     queryStrategy
	Clients         []*Client
	DownClients     []*Client
	lruExpiresCache *LEC.LruExpiresCache
	weightSum       int
	clientsLock     sync.RWMutex
	crontab         *cron.Cron
}

func createClients(clientsConfig []*ClientConfig) []*Client {
	var ret []*Client
	for _, config := range clientsConfig {
		d, err := dns.NewClient(config.URL)
		if err != nil {
			log.Println(err.Error())
			continue
		}

		s := new(Client)
		s.c = d
		s.ClientConfig = config
		ret = append(ret, s)
	}

	return ret
}

func NewResolver(config *Config) (r *Resolver, err error) {
	r = new(Resolver)

	r.Clients = createClients(config.ClientsConfig)

	if config.Cache {
		lruExpiresCache, err := LEC.New(4096)
		if err != nil {
			log.Println(err)
			return nil, err
		}
		r.lruExpiresCache = lruExpiresCache
	}

	switch config.Strategy {
	case "concurrent", "":
		r.StrategyFun = concurrentQuery
	case "random":
		r.StrategyFun = randomQuery
	case "load-balanced":
		var weights []int
		for i := len(r.Clients) - 1; i >= 0; i-- {
			c := r.Clients[i]
			if c.Weight == 0 {
				log.Printf("Strategy is load-balanced, but the dns server %s has no valid weight\n", c.URL)
				r.Clients = append(r.Clients[:i], r.Clients[i+1:]...)
			} else {
				weights = append(weights, c.Weight)
			}
		}

		n := gcdN(weights)
		for _, c := range r.Clients {
			c.Weight = c.Weight / n
			r.weightSum += c.Weight
		}
		r.StrategyFun = loadBalancedQuery
	default:
		return nil, errors.New(fmt.Sprintf("Invalid strategy: %s", config.Strategy))
	}

	r.crontab = cron.New()
	_, _ = r.crontab.AddFunc("@every 300s", r.recoverClient)
	r.crontab.Start()

	return
}

func (r *Resolver) Exchange(m *D.Msg) (msg *D.Msg, err error) {
	if len(m.Question) == 0 {
		return nil, errors.New("should have one question at least")
	}

	q := m.Question[0]

	h, hit := r.Hosts.queryHosts(q.String())
	if hit {
		msg = h.Copy()
		return
	}

	if r.lruExpiresCache != nil {
		cache, expireTime, hit := r.lruExpiresCache.Get(q.String())
		if hit {
			now := time.Now()
			msg = cache.(*D.Msg).Copy()
			if expireTime.Before(now) {
				setMsgTTL(msg, uint32(1))
				go func() {
					update, err := r.queryUpstream(m)
					if err != nil {
						log.Println(err)
					}
					putMsgToCache(r.lruExpiresCache, m.Question[0].String(), update)
				}()
			} else {
				setMsgTTL(msg, uint32(time.Until(expireTime).Seconds()))
			}
		} else {
			msg, err = r.queryUpstream(m)
			putMsgToCache(r.lruExpiresCache, m.Question[0].String(), msg)
		}
		return
	}

	return r.queryUpstream(m)
}

func (r *Resolver) queryUpstream(m *D.Msg) (msg *D.Msg, err error) {
	if e := m.IsEdns0(); e != nil {
		e.SetUDPSize(4096)
	} else {
		m.SetEdns0(4096, false)
	}

	msg, err = r.StrategyFun(m, r)

	return
}

func (r *Resolver) ResolveHost(host string) (ip net.IP, err error) {
	ip = net.ParseIP(host)
	if ip != nil {
		return ip, nil
	}

	m := new(D.Msg)
	m.SetQuestion(D.Fqdn(host), D.TypeA)

	msg, err := r.Exchange(m)
	if err != nil {
		return nil, err
	}

	for _, answer := range msg.Answer {
		switch answer2 := answer.(type) {
		case *D.A:
			ip = answer2.A
		case *D.AAAA:
			ip = answer2.AAAA
		}
	}

	if ip == nil {
		return nil, errors.New("can not resolve ip")
	}

	return
}

func (r *Resolver) ListenHostsFile(hostsFile string) {
	listenHostsFile(r, hostsFile)
}
