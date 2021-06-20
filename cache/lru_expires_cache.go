package lru_expires_cache

import (
	"time"

	"github.com/hashicorp/golang-lru"
)

type valueIncludeExpires struct {
	data    interface{}
	expires time.Time
}

type LruExpiresCache struct {
	*lru.Cache
}

func New(size int) (*LruExpiresCache, error) {
	lruCache, err := lru.New(size)
	if err != nil {
		return nil, err
	}
	lruExpiresCache := &LruExpiresCache{lruCache}
	return lruExpiresCache, nil
}

func (lec *LruExpiresCache) Get(key interface{}) (interface{}, time.Time, bool) {
	entry, ok := lec.Cache.Get(key)
	if !ok {
		return nil, time.Time{}, false
	}
	e := entry.(*valueIncludeExpires)
	return e.data, e.expires, true
}

func (lec *LruExpiresCache) Add(key interface{}, value interface{}, expires time.Time) bool {
	v := &valueIncludeExpires{
		value,
		expires,
	}
	return lec.Cache.Add(key, v)
}
