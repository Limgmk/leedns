package resolver

import (
	"context"
	"math/rand"

	D "github.com/miekg/dns"
)

const maxFailedTimes = 5

func (r *Resolver) copyClients() []*Client {
	r.lock.RLock()
	clients := make([]*Client, len(r.Clients))
	copy(clients, r.Clients)
	r.lock.RUnlock()
	return clients
}

func (r *Resolver) failedClient(c *Client) {
	c.lock.Lock()
	if c.failedTimes < maxFailedTimes-1 {
		c.failedTimes++
		c.lock.Unlock()
		return
	} else if c.failedTimes == maxFailedTimes-1 {
		c.failedTimes++
		c.lock.Unlock()
		r.lock.Lock()
		r.DownClients = append(r.DownClients, c)
		if len(r.DownClients) == len(r.Clients) {
			for _, c := range r.Clients {
				c.failedTimes = 0
			}
			r.DownClients = []*Client{}
		}
		r.lock.Unlock()
		return
	}
	c.lock.Unlock()
}

func (r *Resolver) recoverClient() {
	r.lock.Lock()
	for i := len(r.DownClients) - 1; i >= 0; i-- {
		c := r.DownClients[i]
		m := new(D.Msg)
		m.SetQuestion(D.Fqdn("domain.com"), D.TypeA)
		_, _, err := c.c.Exchange(m)
		if err == nil {
			c.lock.Lock()
			c.failedTimes = 0
			c.lock.Unlock()
			r.DownClients = append(r.DownClients[:i], r.DownClients[i+1:]...)
		}
	}
	r.lock.Unlock()
}

func (r *Resolver) getClientByLoad() (c *Client, index int) {
	r.lock.Lock()
	maxItemIndex := 0
	for index, c := range r.Clients {
		if c.currentWeight > r.Clients[maxItemIndex].currentWeight {
			maxItemIndex = index
		}
		c.currentWeight += c.Weight
	}
	c = r.Clients[maxItemIndex]
	c.currentWeight = c.currentWeight - r.weightSum
	r.lock.Unlock()

	return c, maxItemIndex
}

func concurrentQuery(m *D.Msg, r *Resolver) (msg *D.Msg, err error) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		Msg   *D.Msg
		Error error
	}

	r.lock.RLock()
	ch := make(chan result, len(r.Clients))
	clients := make([]*Client, len(r.Clients))
	copy(clients, r.Clients)
	r.lock.RUnlock()
	for _, c := range clients {
		c := c
		if c.failedTimes == maxFailedTimes {
			continue
		}
		go func() {
			msg, _, err := c.c.ExchangeContext(ctx, m)
			ch <- result{msg, err}
			if err != nil {
				r.failedClient(c)
			}
		}()
	}

	var badResult *D.Msg
	for i := 0; i < len(clients); i++ {
		ret := <-ch
		msg = ret.Msg
		err = ret.Error
		if err != nil || msg == nil {
			continue
		}
		if msg.Answer != nil {
			break
		} else {
			badResult = msg
		}
	}

	if msg == nil {
		msg = badResult
	}

	return msg, err
}

func randomQuery(m *D.Msg, r *Resolver) (msg *D.Msg, err error) {

	var badResult *D.Msg
	clients := r.copyClients()
	for i := len(clients) - 1; i >= 0; i-- {
		randIndex := rand.Intn(len(clients))
		c := clients[randIndex]

		if c.failedTimes == maxFailedTimes {
			clients = append(clients[:randIndex], clients[randIndex+1:]...)
			continue
		}

		msg, _, err = c.c.Exchange(m)
		if err != nil || msg == nil {
			r.failedClient(c)
			clients = append(clients[:randIndex], clients[randIndex+1:]...)
			continue
		}
		if msg.Answer != nil {
			break
		} else {
			badResult = msg
		}
	}

	if msg == nil {
		msg = badResult
	}

	return msg, err
}

func loadBalancedQuery(m *D.Msg, r *Resolver) (msg *D.Msg, err error) {

	var badResult *D.Msg
	var lastClient *Client
	len := len(r.Clients)
	for i := len - 1; i >= 0; {
		c, _ := r.getClientByLoad()

		if c.failedTimes == maxFailedTimes {
			continue
		}

		if c != lastClient {
			i--
			lastClient = c
		}

		msg, _, err = c.c.Exchange(m)
		if err != nil || msg == nil {
			r.failedClient(c)
			continue
		}
		if msg.Answer != nil {
			break
		} else {
			badResult = msg
		}
	}

	if msg == nil {
		msg = badResult
	}

	return msg, err
}

func fallbackQuery(m *D.Msg, r *Resolver) (msg *D.Msg, err error) {
	var badResult *D.Msg
	for _, c := range r.Clients {
		if c.failedTimes == maxFailedTimes {
			continue
		}

		msg, _, err = c.c.Exchange(m)
		if err != nil || msg == nil {
			r.failedClient(c)
			continue
		}
		if msg.Answer != nil {
			break
		} else {
			badResult = msg
		}
	}

	if msg == nil {
		msg = badResult
	}

	return msg, err
}
