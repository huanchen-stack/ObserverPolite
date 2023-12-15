package mongodb

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	cm "observerPolite/common"
	"reflect"
	"sync"
	"time"
)

type FutureResult struct {
	resultChan chan interface{}
}

func (f *FutureResult) Await() interface{} {
	result := <-f.resultChan
	return result
}

type DBRequest struct {
	Key        string
	Value      string
	ResultChan chan interface{}
}

type DBDoc interface {
	GetURL() string
}

type DBAccess interface {
	GetCtx() context.Context
	GetCollection() *mongo.Collection
	GetBatchRead() *[]DBRequest
	GetBatchMutex() *sync.Mutex
}

func BulkRead[T DBDoc](dbAccess DBAccess, key string, vals []string) []T {
	collection := dbAccess.GetCollection()
	ctx := dbAccess.GetCtx()
	filter := bson.M{key: bson.M{"$in": vals}}

	cursor, err := collection.Find(ctx, filter)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return make([]T, 0)
		} else {
			fmt.Println("DB Bulk Read Err") // TODO: fix this
			return make([]T, 0)
		}
	}
	defer cursor.Close(ctx)

	var results []T
	if err := cursor.All(ctx, &results); err != nil {
		fmt.Println("DB Bulk Read Decode Err")
	}

	return results
}

func BatchProcessor[T DBDoc](dbAccess DBAccess) {
	ticker := time.NewTicker(cm.GlobalConfig.DBWriteFrequency)

	for range ticker.C {
		batch := dbAccess.GetBatchRead()
		mutex := dbAccess.GetBatchMutex()
		var tmp T
		fmt.Println(reflect.TypeOf(tmp), "len readbatch: ", len(*batch))

		mutex.Lock()
		currentBatchChanMap := make(map[string][]chan interface{}, 0) // concurrent robotstxt lookup to the same host
		for i, _ := range *batch {
			currentBatchChanMap[(*batch)[i].Value] = append(currentBatchChanMap[(*batch)[i].Value], (*batch)[i].ResultChan)
		}
		//if len(currentBatchChanMap) != len(currentBatch) {
		//	panic("duplicate url input!!!")
		//}
		*batch = (*batch)[:0]
		mutex.Unlock()

		currentBatch := make([]string, len(currentBatchChanMap))
		idx := 0
		for k, _ := range currentBatchChanMap {
			currentBatch[idx] = k
			idx += 1
		}
		if len(currentBatch) == 0 {
			continue
		}
		results := BulkRead[T](dbAccess, "url", currentBatch)

		for i, _ := range results {
			for j, _ := range currentBatchChanMap[results[i].GetURL()] {
				currentBatchChanMap[results[i].GetURL()][j] <- results[i]
			}
			delete(currentBatchChanMap, results[i].GetURL())
		}
		for _, v := range currentBatchChanMap {
			var tmp T
			for i, _ := range v {
				v[i] <- tmp
			}
		}
	}
}

func GetOneAsync(key string, val string, readBatch *[]DBRequest, mutex *sync.Mutex) *FutureResult {
	resultChan := make(chan interface{}, 1)

	mutex.Lock()
	*readBatch = append(*readBatch, DBRequest{
		Key: key, Value: val,
		ResultChan: resultChan,
	})
	mutex.Unlock()

	return &FutureResult{resultChan: resultChan}
}
