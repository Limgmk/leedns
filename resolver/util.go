package resolver

import (
	"time"

	LEC "github.com/Limgmk/leedns/cache"
	D "github.com/miekg/dns"
)

// The functions setMsgTTL() and putMsgToCache()
// reused https://github.com/Dreamacro/clash/blob/master/dns/util.go

func setMsgTTL(msg *D.Msg, ttl uint32) {
	for _, answer := range msg.Answer {
		answer.Header().Ttl = ttl
	}

	for _, ns := range msg.Ns {
		ns.Header().Ttl = ttl
	}

	for _, extra := range msg.Extra {
		extra.Header().Ttl = ttl
	}
}

func putMsgToCache(cache *LEC.LruExpiresCache, key string, msg *D.Msg) {
	if msg == nil {
		return
	}

	var ttl uint32
	switch {
	case len(msg.Answer) != 0:
		ttl = msg.Answer[0].Header().Ttl
	case len(msg.Ns) != 0:
		ttl = msg.Ns[0].Header().Ttl
	case len(msg.Extra) != 0:
		ttl = msg.Extra[0].Header().Ttl
	default:
		return
	}

	cache.Add(key, msg.Copy(), time.Now().Add(time.Second*time.Duration(ttl)))
}

func gcdN(digits []int) int {
	l := len(digits)
	if l == 1 {
		return digits[0]
	}

	var gcd func(a, b int) int
	gcd = func(a, b int) int {
		if b == 0 {
			return a
		}
		return gcd(b, a%b)
	}

	return gcd(digits[l-1], gcdN(digits[:l-1]))
}
