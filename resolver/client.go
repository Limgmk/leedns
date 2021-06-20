package resolver

import (
	"context"
	"math/rand"

	D "github.com/miekg/dns"
)

func (c *Client) getFailedTimes() int {
	c.lock.RLock()
	times := c.failedTimes
	c.lock.RUnlock()
	return times
}

func (c *Client) addFailedTimes() {
	c.lock.RLock()
	c.failedTimes++
	c.lock.RUnlock()
}

func (r *Resolver) copyClients() []*Client {
	r.clientsLock.RLock()
	clients := make([]*Client, len(r.Clients))
	copy(clients, r.Clients)
	r.clientsLock.RUnlock()
	return clients
}

func (r *Resolver) removeClient(c *Client) {
	r.clientsLock.Lock()
	for i := len(r.Clients) - 1; i >= 0; i-- {
		item := r.Clients[i]
		if item == c {
			r.weightSum = r.weightSum - c.Weight
			r.Clients = append(r.Clients[:i], r.Clients[i+1:]...)
			r.DownClients = append(r.DownClients, c)
			break
		}
	}
	r.clientsLock.Unlock()
}

func (r *Resolver) resetClients() {
	r.clientsLock.Lock()
	r.weightSum = 0
	for _, c := range r.Clients {
		c.currentWeight = 0
		c.failedTimes = 0
		r.weightSum += c.Weight
	}
	for _, c := range r.DownClients {
		c.currentWeight = 0
		c.failedTimes = 0
		r.weightSum += c.Weight
	}
	r.Clients = append(r.Clients, r.DownClients...)
	r.DownClients = []*Client{}
	r.clientsLock.Unlock()
}

func (r *Resolver) getClientsLen() int {
	r.clientsLock.RLock()
	l := len(r.Clients)
	r.clientsLock.RUnlock()
	return l
}

func (r *Resolver) failedClient(c *Client) {
	if c.getFailedTimes() < 5 {
		c.addFailedTimes()
		return
	}
	if r.getClientsLen() <= 1 {
		r.resetClients()
		return
	}
	r.removeClient(c)
}

func (r *Resolver) recoverClient() {
	r.clientsLock.Lock()
	for i := len(r.DownClients) - 1; i >= 0; i-- {
		c := r.DownClients[i]
		m := new(D.Msg)
		m.SetQuestion(D.Fqdn("domain.com"), D.TypeA)
		_, _, err := c.c.Exchange(m)
		if err == nil {
			c.failedTimes = 0
			r.DownClients = append(r.DownClients[:i], r.DownClients[i+1:]...)
			r.Clients = append(r.Clients, c)
		}
	}
	r.clientsLock.Unlock()
}

func (r *Resolver) getClientByLoad(clients []*Client) (c *Client, index int) {
	r.clientsLock.Lock()
	maxItemIndex := 0
	for index, c := range clients {
		if c.currentWeight > clients[maxItemIndex].currentWeight {
			maxItemIndex = index
		}
		c.currentWeight += c.Weight
	}
	c = clients[maxItemIndex]
	c.currentWeight = c.currentWeight - r.weightSum
	r.clientsLock.Unlock()

	return c, maxItemIndex
}

func concurrentQuery(m *D.Msg, r *Resolver) (msg *D.Msg, err error) {

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type result struct {
		Msg   *D.Msg
		Error error
	}

	r.clientsLock.RLock()
	ch := make(chan result, len(r.Clients))
	clients := make([]*Client, len(r.Clients))
	copy(clients, r.Clients)
	r.clientsLock.RUnlock()
	for _, c := range clients {
		c := c
		go func() {
			msg, _, err := c.c.ExchangeContext(ctx, m)
			ch <- result{msg, err}
			if err != nil {
				r.failedClient(c)
			}
		}()
	}

	var ret result
	for i := 0; i < len(clients); i++ {
		if ret = <-ch; ret.Msg == nil || ret.Msg.Answer == nil {
			continue
		} else {
			break
		}
	}

	return ret.Msg, ret.Error
}

func randomQuery(m *D.Msg, r *Resolver) (msg *D.Msg, err error) {

	clients := r.copyClients()
	for i := len(clients) - 1; i >= 0; i-- {
		randIndex := rand.Intn(len(clients))
		c := clients[randIndex]

		msg, _, err = c.c.Exchange(m)
		if err == nil {
			break
		} else {
			r.failedClient(c)
			clients = append(clients[:randIndex], clients[randIndex+1:]...)
		}
	}

	return msg, err
}

func loadBalancedQuery(m *D.Msg, r *Resolver) (msg *D.Msg, err error) {

	clients := r.copyClients()
	for i := len(clients) - 1; i >= 0; i-- {

		c, maxItemIndex := r.getClientByLoad(clients)

		msg, _, err = c.c.Exchange(m)
		if err == nil {
			break
		} else {
			r.failedClient(c)
			clients = append(clients[:maxItemIndex], clients[maxItemIndex+1:]...)
		}
	}

	return msg, err
}

