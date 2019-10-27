package layer

import (
	"io"
	"time"
)

//A CacheLayer stores and retrives cached responses.
// The cache may delete a entry at any point which is required by some cache replacement policies
// The TTL of a cached entry is a guide which can be used by the cache replacement policy
// It is recommended to keep stale content for as long as capacity of the storage backend allows
//
// All actions of a cache layer must be safe for concurrent use by multiple goroutines.
type CacheLayer interface {

	//Get requests a stored object from the cache with the cache key 'key'.
	// If there is no response with that key nil should be returned
	// If there is a response the response and the TTL should be returned
	// Error should only be returned in case of a error while getting the data like a connection error to a storage backend
	Get(key string) (io.ReadCloser, time.Duration, error)

	//Set a new cache entry. if a key is already in use it should be overwritten.
	Set(key string, entry io.ReadCloser, ttl time.Duration) error

	//Update the ttl of a existing cache entry
	Refresh(key string, ttl time.Duration) error

	//Delete a cache entry with the given key
	Delete(key string) error
}
