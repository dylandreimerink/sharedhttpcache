package layer

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"sync"
	"time"
)

//The InMemoryCacheLayer stores responses in memory
// This is a simple example without advanced features
type InMemoryCacheLayer struct {
	//Maximum size of the cache in bytes
	MaxSize int

	entityStore map[string]inMemoryCacheEntity

	currentSize int

	lock sync.RWMutex

	staleKeys map[string]bool
}

type inMemoryCacheEntity struct {
	Data       []byte
	Expiration time.Time
}

func NewInMemoryCacheLayer(maxSize int) *InMemoryCacheLayer {
	return &InMemoryCacheLayer{
		MaxSize:     maxSize,
		entityStore: make(map[string]inMemoryCacheEntity, 500),
		staleKeys:   make(map[string]bool, 100),
	}
}

func (layer *InMemoryCacheLayer) Get(key string) (io.ReadCloser, time.Duration, error) {
	layer.lock.RLock()
	defer layer.lock.RUnlock()

	if entity, found := layer.entityStore[key]; found {
		ttl := time.Until(entity.Expiration)

		//If entry is stale
		if ttl <= 0 {
			layer.staleKeys[key] = true
		}

		return ioutil.NopCloser(bytes.NewReader(entity.Data)), ttl, nil
	}

	return nil, 0, nil
}

func (layer *InMemoryCacheLayer) Set(key string, entry io.ReadCloser, ttl time.Duration) error {
	entryBytes, err := ioutil.ReadAll(entry)
	defer entry.Close()

	if err != nil {
		return err
	}

	layer.lock.Lock()
	defer layer.lock.Unlock()

	availableRoom := (layer.MaxSize - layer.currentSize)
	//If the entry is bigger than the max size we have to make room
	if len(entryBytes) > availableRoom {
		err := layer.replaceCache(availableRoom - len(entryBytes))
		if err != nil {
			return err
		}
	}

	return layer.set(key, inMemoryCacheEntity{
		Data:       entryBytes,
		Expiration: time.Now().Add(ttl),
	})
}

func (layer *InMemoryCacheLayer) Delete(key string) error {
	layer.lock.Lock()
	layer.delete(key)
	layer.lock.Unlock()
	return nil
}

func (layer *InMemoryCacheLayer) Refresh(key string, ttl time.Duration) error {
	layer.lock.Lock()
	defer layer.lock.Unlock()

	if entity, found := layer.entityStore[key]; found {
		entity.Expiration = time.Now().Add(ttl)
		layer.entityStore[key] = entity
	}

	return fmt.Errorf("Entity with key '%s' doesn't exist", key)
}

//WARNING call this function only when the layer is already write locked
func (layer *InMemoryCacheLayer) replaceCache(neededSize int) error {

	//Loop over all known stale keys and remove them until we have room or there are no more stale keys
	for key := range layer.staleKeys {
		neededSize -= layer.delete(key)

		delete(layer.staleKeys, key)

		//If we have enough space we return
		if neededSize <= 0 {
			return nil
		}
	}

	//If we still need room and there are no stale keys start removing fresh entries
	for key := range layer.entityStore {
		neededSize -= layer.delete(key)

		//If we have enough space we return
		if neededSize <= 0 {
			return nil
		}
	}

	return errors.New("Can't make enough room")
}

func (layer *InMemoryCacheLayer) delete(key string) int {
	if entry, found := layer.entityStore[key]; found {
		size := len(entry.Data)

		delete(layer.entityStore, key)

		layer.currentSize -= size

		return size
	}

	return 0
}

func (layer *InMemoryCacheLayer) set(key string, entry inMemoryCacheEntity) error {
	//Delete the key first so the current size is updated
	layer.delete(key)

	layer.currentSize += len(entry.Data)
	layer.entityStore[key] = entry

	return nil
}
