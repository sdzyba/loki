package lockotron

import (
	"errors"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("cached value not found")
)

type Cache struct {
	locker   *locker
	mutex    sync.RWMutex
	items    map[string]*item
	stopChan chan bool
	ticker   *time.Ticker
	config   *Config
}

func NewCache(config *Config) *Cache {
	c := &Cache{
		locker: newLocker(),
		items:  make(map[string]*item),
		config: config,
	}

	if config.CleanupInterval != NoCleanup {
		c.ticker = time.NewTicker(config.CleanupInterval)

		go func() {
			for {
				select {
				case <-c.ticker.C:
					c.DeleteExpired()
				case <-c.stopChan:
					c.ticker.Stop()

					return
				}
			}
		}()
	}

	return c
}

func (c *Cache) Close() error {
	if c.stopChan == nil || c.ticker == nil {
		return nil
	}

	close(c.stopChan)

	return nil
}

func (c *Cache) Set(key string, value interface{}) {
	c.set(key, c.config.DefaultTTL, value)
}

func (c *Cache) SetList(list map[string]interface{}) {
	c.mutex.Lock()
	for key, value := range list {
		c.items[key] = newItem(value, c.config.DefaultTTL)
	}
	c.mutex.Unlock()
}

func (c *Cache) SetEx(key string, ttl time.Duration, value interface{}) {
	c.set(key, ttl, value)
}

func (c *Cache) Get(key string) (interface{}, error) {
	c.mutex.RLock()
	item, ok := c.items[key]
	c.mutex.RUnlock()
	if ok {
		return item.value, nil
	}

	return nil, ErrNotFound
}

func (c *Cache) GetList(keys []string) []interface{} {
	values := make([]interface{}, 0, len(keys))

	c.mutex.RLock()
	for _, key := range keys {
		item, ok := c.items[key]
		if ok {
			values = append(values, item.value)
		}
	}
	c.mutex.RUnlock()

	return values
}

func (c *Cache) Delete(key string) {
	c.mutex.Lock()
	delete(c.items, key)
	c.mutex.Unlock()
}

func (c *Cache) Fetch(key string, fallback func(string) (interface{}, error)) (interface{}, error) {
	return c.fetch(key, c.config.DefaultTTL, fallback)
}

func (c *Cache) FetchEx(key string, ttl time.Duration, fallback func(string) (interface{}, error)) (interface{}, error) {
	return c.fetch(key, ttl, fallback)
}

func (c *Cache) DeleteAll() {
	c.mutex.Lock()
	c.items = make(map[string]*item)
	c.mutex.Unlock()
}

func (c *Cache) DeleteExpired() {
	now := time.Now().UnixNano()

	c.mutex.Lock()
	for key, item := range c.items {
		if item.isExpirable() && now > item.ttl {
			delete(c.items, key)
		}
	}
	c.mutex.Unlock()
}

func (c *Cache) DeleteList(keys []string) {
	c.mutex.Lock()
	for _, key := range keys {
		delete(c.items, key)
	}
	c.mutex.Unlock()
}

func (c *Cache) fetch(key string, ttl time.Duration, fallback func(string) (interface{}, error)) (interface{}, error) {
	value, err := c.Get(key)
	if err == nil {
		return value, nil
	}

	mutex := c.locker.obtain(key)
	mutex.Lock()
	defer mutex.Unlock()
	defer c.locker.release(key)

	value, err = c.Get(key)
	if err == nil {
		return value, nil
	}

	value, err = fallback(key)
	if err != nil {
		return nil, err
	}

	c.SetEx(key, ttl, value)

	return value, nil
}

func (c *Cache) set(key string, ttl time.Duration, value interface{}) {
	c.mutex.Lock()
	c.items[key] = newItem(value, ttl)
	c.mutex.Unlock()
}

func IsNotFoundErr(err error) bool {
	return ErrNotFound == err
}
