package game

import "testing"

func T(s Suit, n int) Tile { return Tile{Suit: s, Num: n} }

func TestSevenPairs(t *testing.T) {
	hand := []Tile{
		T(Wan,1),T(Wan,1), T(Wan,2),T(Wan,2), T(Tong,3),T(Tong,3),
		T(Tiao,4),T(Tiao,4), T(Feng,1),T(Feng,1), T(Jian,1),T(Jian,1),
		T(Wan,9),T(Wan,9),
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
	// wan 147, tong 258, tiao 369, 5 honors
	hand := []Tile{
		T(Wan,1),T(Wan,4),T(Wan,7),
		T(Tong,2),T(Tong,5),T(Tong,8),
		T(Tiao,3),T(Tiao,6),T(Tiao,9),
		T(Feng,1),T(Feng,2),T(Feng,3),T(Feng,4),T(Jian,1),
	}
	if !CanThirteenOrphansLike(hand) {
		t.Fatal("thirteen")
	}
	if PatternMultiplier(hand, nil) != 2 {
		t.Fatal("mul")
	}
}

func TestQingSeven(t *testing.T) {
	hand := []Tile{
		T(Wan,1),T(Wan,1), T(Wan,2),T(Wan,2), T(Wan,3),T(Wan,3),
		T(Wan,4),T(Wan,4), T(Wan,5),T(Wan,5), T(Wan,6),T(Wan,6),
		T(Wan,7),T(Wan,7),
	}
	if PatternMultiplier(hand, nil) != 4 {
		t.Fatalf("want 4 got %d", PatternMultiplier(hand, nil))
	}
}

func TestHorse(t *testing.T) {
	tiles := []Tile{T(Wan,1),T(Wan,2),T(Tiao,5),T(Feng,1),T(Tong,9),T(Tong,3)}
	if CountValidHorses(tiles) != 3 {
		t.Fatal("horses")
	}
}

func TestKongFees(t *testing.T) {
	// an: seat0, n=4 => +6, others -2
	d := ScoreKongFees(4, []KongRecord{{Seat: 0, Kind: "an", From: -1}})
	if d[0] != 6 || d[1] != -2 {
		t.Fatalf("%v", d)
	}
	// ming: from 1 to 0 => +3/-3
	d = ScoreKongFees(4, []KongRecord{{Seat: 0, Kind: "ming", From: 1}})
	if d[0] != 3 || d[1] != -3 {
		t.Fatalf("%v", d)
	}
}