package surebankltd

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"

	"cloud.google.com/go/firestore"
	"google.golang.org/api/iterator"
)

// Counter is a collection of documents (shards)
// to realize counter with high frequency.
type Counter struct {
	numShards int
}

// Shard is a single counter, which is used in a group
// of other shards within Counter.
type Shard struct {
	Count int
}

// initCounter creates a given number of shards as
// subcollection of specified document.
func initCounter(ctx context.Context, numShards int, docRef *firestore.DocumentRef) (*Counter, error) {
	if _, err := docRef.Get(ctx); err == nil {
		return nil, nil // shards already exists
	}
	colRef := docRef.Collection("shards")

	c := new(Counter)
	// Initialize each shard with count=0
	for num := 0; num < c.numShards; num++ {
		shard := Shard{0}

		if _, err := colRef.Doc(strconv.Itoa(num)).Set(ctx, shard); err != nil {
			return nil, fmt.Errorf("Set: %v", err)
		}
	}
	return c, nil
}

// incrementCounter increments a randomly picked shard.
func (c *Counter) incrementCounter(ctx context.Context, docRef *firestore.DocumentRef, inc interface{}, batch *firestore.WriteBatch) (*firestore.WriteBatch) {
	docID := strconv.Itoa(rand.Intn(c.numShards))

	shardRef := docRef.Collection("shards").Doc(docID)
	batch = batch.Update(shardRef, []firestore.Update{
		{Path: "Count", Value: firestore.Increment(inc)},
	})

	return batch
}

// getCount returns a total count across all shards.
func getCount(ctx context.Context, docRef *firestore.DocumentRef) (int64, error) {
	var total int64
	shards := docRef.Collection("shards").Documents(ctx)
	for {
		doc, err := shards.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("Next: %v", err)
		}

		vTotal := doc.Data()["Count"]
		shardCount, ok := vTotal.(int64)
		if !ok {
			return 0, fmt.Errorf("firestore: invalid dataType %T, want int64", vTotal)
		}
		total += shardCount
	}
	return total, nil
}

// getCount returns a total count across all shards.
func getTotal(ctx context.Context, docRef *firestore.DocumentRef) (float64, error) {
	var total float64
	shards := docRef.Collection("shards").Documents(ctx)
	for {
		doc, err := shards.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			return 0, fmt.Errorf("Next: %v", err)
		}

		vTotal := doc.Data()["Count"]
		shardCount, ok := vTotal.(float64)
		if !ok {
			return 0, fmt.Errorf("firestore: invalid dataType %T, want float64", vTotal)
		}
		total += shardCount
	}
	return total, nil
}
