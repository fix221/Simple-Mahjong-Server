package tuidaohu

import (
	"testing"

	"mahjong/internal/game/universal"
)

func T(s universal.Suit, n int) universal.Tile {
	return universal.Tile{Suit: s, Num: n}
}

func TestSevenPairs(t *testing.T) {
	hand := []universal.Tile{
		T(universal.Wan, 1), T(universal.Wan, 1), T(universal.Wan, 2), T(universal.Wan, 2),
		T(universal.Tong, 3), T(universal.Tong, 3), T(universal.Tiao, 4), T(universal.Tiao, 4),
		T(universal.Feng, 1), T(universal.Feng, 1), T(universal.Jian, 1), T(universal.Jian, 1),
		T(universal.Wan, 9), T(universal.Wan, 9),
	}
	if !CanSevenPairs(hand) {
		t.Fatal("seven pairs")
	}
	if !CanWin(hand) {
		t.Fatal("can win")
	}
	if PatternMultiplier(hand, nil) != 2 {
		t.Fatal("mul")
	}
}

func TestThirteen(t *testing.T) {
	hand := []universal.Tile{
		T(universal.Wan, 1), T(universal.Wan, 4), T(universal.Wan, 7),
		T(universal.Tong, 2), T(universal.Tong, 5), T(universal.Tong, 8),
		T(universal.Tiao, 3), T(universal.Tiao, 6), T(universal.Tiao, 9),
		T(universal.Feng, 1), T(universal.Feng, 2), T(universal.Feng, 3), T(universal.Feng, 4),
		T(universal.Jian, 1),
	}
	if !CanThirteenBukao(hand) {
		t.Fatal("thirteen")
	}
	if PatternMultiplier(hand, nil) != 2 {
		t.Fatal("mul")
	}
}

func TestQingSeven(t *testing.T) {
	hand := []universal.Tile{
		T(universal.Wan, 1), T(universal.Wan, 1), T(universal.Wan, 2), T(universal.Wan, 2),
		T(universal.Wan, 3), T(universal.Wan, 3), T(universal.Wan, 4), T(universal.Wan, 4),
		T(universal.Wan, 5), T(universal.Wan, 5), T(universal.Wan, 6), T(universal.Wan, 6),
		T(universal.Wan, 7), T(universal.Wan, 7),
	}
	if PatternMultiplier(hand, nil) != 12 {
		t.Fatalf("want 12 got %d", PatternMultiplier(hand, nil))
	}
}

func TestHorse(t *testing.T) {
	tiles := []universal.Tile{
		T(universal.Wan, 1), T(universal.Wan, 2), T(universal.Tiao, 5),
		T(universal.Feng, 1), T(universal.Tong, 9), T(universal.Tong, 3),
	}
	if CountValidHorses(tiles) != 3 {
		t.Fatal("horses")
	}
}

func TestKongFees(t *testing.T) {
	d := ScoreKongFees(4, []KongRecord{{Seat: 0, Kind: "an", From: -1}})
	if d[0] != 6 || d[1] != -2 {
		t.Fatalf("%v", d)
	}
	d = ScoreKongFees(4, []KongRecord{{Seat: 0, Kind: "ming", From: 1}})
	if d[0] != 3 || d[1] != -3 {
		t.Fatalf("%v", d)
	}
}