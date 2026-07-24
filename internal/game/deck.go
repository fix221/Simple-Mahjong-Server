package game

import (
	"math/rand"
	"time"
)

func Shuffle(tiles []Tile) {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	r.Shuffle(len(tiles), func(i, j int) {
		tiles[i], tiles[j] = tiles[j], tiles[i]
	})
}
