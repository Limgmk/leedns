package resolver

import (
	"context"
	"math/rand"

	D "github.com/miekg/dns"
)

const maxFailedTimes = 5

// func (c *Client) getFailedTimes() int {
// 	c.lock.RLock()
// 	times := c.failedTimes
// 	c.lock.RUnlock()
// 	return times
// }

// func (c *Client) addFailedTimes() {
// 	c.lock.Lock()
// 	c.failedTimes++
// 	c.lock.Unlock()
// }

func (r *Resolver) copyClients() []*Client {
	r.lock.RLock()
	clients := make([]*Client, len(r.Clients))
	copy(clients, r.Clients)
	r.lock.RUnlock()
	return clients
}

// func (r *Resolver) removeClient(c *Client) {
// 	r.clientsLock.Lock()
// 	for i := len(r.Clients) - 1; i >= 0; i-- {
// 		item := r.Clients[i]
// 		if item == c {
// 			r.weightSum = r.weightSum - c.Weight
// 			r.Clients = append(r.Clients[:i], r.Clients[i+1:]...)
// 			r.DownClients = append(r.DownClients, c)
// 			break
// 		}
// 	}
// 	r.clientsLock.Unlock()
// }

// func (r *Resolver) resetClients() {
// 	r.clientsLock.Lock()
// 	r.weightSum = 0
// 	for _, c := range r.Clients {
// 		c.currentWeight = 0
// 		c.failedTimes = 0
// 		r.weightSum += c.Weight
// 	}
// 	for _, c := range r.DownClients {
// 		c.currentWeight = 0
// 		c.failedTimes = 0
// 		r.weightSum += c.Weight
// 	}
// 	r.Clients = append(r.Clients, r.DownClients...)
// 	r.DownClients = []*Client{}
// 	r.clientsLock.Unlock()
// }

// func (r *Resolver) resetClients() {
// 	r.lock.Lock()
// 	r.weightSum = 0
// 	for _, c := range r.Clients {
// 		c.currentWeight = 0
// 		c.failedTimes = 0
// 		r.weightSum += c.Weight
// 	}
// 	r.DownClients = []*Client{}
// 	r.lock.Unlock()
// }

// func (r *Resolver) getClientsLen() int {
// 	r.clientsLock.RLock()
// 	l := len(r.Clients)
// 	r.clientsLock.RUnlock()
// 	return l
// }

// func (r *Resolver) getDownClientsLen() int {
// 	r.lock.RLock()
// 	l := len(r.DownClients)
// 	r.lock.RUnlock()
// 	return l
// }

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
				c.lock.Lock()
				c.failedTimes++
				c.lock.Unlock()
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

		if c.failedTimes == maxFailedTimes {
			clients = append(clients[:randIndex], clients[randIndex+1:]...)
			continue
		}

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

	var lastClient *Client
	clients := r.copyClients()
	for i := len(clients) - 1; i >= 0; {
		c, _ := r.getClientByLoad()

		if c.failedTimes == maxFailedTimes {
			continue
		}

		if c != lastClient {
			i--
			lastClient = c
		}

		msg, _, err = c.c.Exchange(m)
		if err == nil {
			break
		} else {
			r.failedClient(c)
		}
	}

	return msg, err
}

// func fallbackQuery(m *D.Msg, r *Resolver) (msg *D.Msg, err error) {

// }
