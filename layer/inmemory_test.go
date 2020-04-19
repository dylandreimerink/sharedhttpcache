package layer

import (
	"io/ioutil"
	"reflect"
	"strings"
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
		return
	}

	if !reflect.DeepEqual(content, []byte("Content")) {
		t.Errorf("Content of key is not equal, expected: %v, got %v", []byte("Content"), content)
		return
	}
}

func TestInMemoryCacheLayer_Delete(t *testing.T) {
	layer := NewInMemoryCacheLayer(1024)

	expiration := time.Now().Add(1 * time.Minute)

	layer.entityStore["key1"] = inMemoryCacheEntity{
		Expiration: expiration,
		Data:       []byte("Content"),
	}
	layer.currentSize = len([]byte("Content"))

	if err := layer.Delete("key1"); err != nil {
		t.Error(err)
		return
	}

	if _, found := layer.entityStore["key1"]; found {
		t.Error("Keys still exists after deleting")
		return
	}
}

func TestInMemoryCacheLayer_Refresh(t *testing.T) {
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

	err = layer.Refresh("key1", 2*time.Minute)
	if err != nil {
		t.Errorf("Error while refreshing key: %s", err)
	}

	reader, duration, err = layer.Get("key1")
	if reader == nil {
		t.Error("Reader of object is nil")
		return
	}

	if !(duration > (119*time.Second) && duration < (120*time.Second)) {
		t.Errorf("Test duration is not 2 minutes, expected: %v, got: %v", (2 * time.Minute), duration)
		return
	}

	if err != nil {
		t.Errorf("Error while getting key: %s", err)
		return
	}

	content, err := ioutil.ReadAll(reader)
	if err != nil {
		t.Errorf("Error while reading from reader: %s", err)
		return
	}

	if !reflect.DeepEqual(content, []byte("Content")) {
		t.Errorf("Content of key is not equal, expected: %v, got %v", []byte("Content"), content)
		return
	}
}

func TestInMemoryCacheLayer_Set(t *testing.T) {
	layer := NewInMemoryCacheLayer(16)

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

	expiration := time.Now().Add(-1 * time.Minute)

	layer.entityStore["key1"] = inMemoryCacheEntity{
		Expiration: expiration,
		Data:       []byte("Stale Content"),
	}
	layer.currentSize = len([]byte("Stale Content"))

	err = layer.Set("key2", ioutil.NopCloser(strings.NewReader("New content")), 1*time.Minute)
	if err != nil {
		t.Errorf("Error while setting key: %s", err)
	}

	reader, _, err = layer.Get("key1")
	if reader != nil {
		t.Error("Stale key still exists after exceeding layer max size")
		return
	}

	if err != nil {
		t.Errorf("Error while getting key: %s", err)
		return
	}

	reader, duration, err = layer.Get("key2")
	if reader == nil {
		t.Error("Reader of object is nil")
		return
	}

	if !(duration > (59*time.Second) && duration < (60*time.Second)) {
		t.Errorf("Test duration is not 2 minutes, expected: %v, got: %v", (2 * time.Minute), duration)
		return
	}

	if err != nil {
		t.Errorf("Error while getting key: %s", err)
		return
	}

	content, err := ioutil.ReadAll(reader)
	if err != nil {
		t.Errorf("Error while reading from reader: %s", err)
		return
	}

	if !reflect.DeepEqual(content, []byte("New content")) {
		t.Errorf("Content of key is not equal, expected: %v, got %v", []byte("New content"), content)
		return
	}
}
