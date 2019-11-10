package layer

import (
	"io/ioutil"
	"reflect"
	"testing"
	"time"
)

func TestInMemoryCacheLayer_Get(t *testing.T) {
	layer := NewInMemoryCacheLayer(1024)

	reader, duration, err := layer.Get("key1")
	if reader != nil {
		t.Error("Reader of non existing object should be nil")
		return
	}

	if duration != 0 {
		t.Error("Duration of non existent object should be 0")
		return
	}

	if err != nil {
		t.Errorf("Error while getting key: %s", err)
	}

	expiration := time.Now().Add(1 * time.Minute)

	layer.entityStore["key1"] = inMemoryCacheEntity{
		Expiration: expiration,
		Data:       []byte("Content"),
	}
	layer.currentSize = len([]byte("Content"))

	reader, duration, err = layer.Get("key1")
	if reader == nil {
		t.Error("Reader of object is nil")
		return
	}

	if !(duration > (59*time.Second) && duration < (60*time.Second)) {
		t.Errorf("Test duration is not 1 minute, expected: %v, got: %v", (1 * time.Minute), duration)
		return
	}

	if err != nil {
		t.Errorf("Error while getting key: %s", err)
		return
	}

	content, err := ioutil.ReadAll(reader)
	if err != nil {
		t.Errorf("Error while reading from reader: %s", err)
	}

	if !reflect.DeepEqual(content, []byte("Content")) {
		t.Errorf("Content of key is not equal, expected: %v, got %v", []byte("Content"), content)
	}
}
