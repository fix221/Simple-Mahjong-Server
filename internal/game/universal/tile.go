package universal

// Suit: 0 wan 1 tong 2 tiao 3 feng 4 jian
type Suit int

const (
	Wan  Suit = 0
	Tong Suit = 1
	Tiao Suit = 2
	Feng Suit = 3
	Jian Suit = 4
)

type Tile struct {
	Suit Suit `json:"suit"`
	Num  int  `json:"num"` // 1-9 / feng 1-4 / jian 1-3
}

func (t Tile) ID() int { return int(t.Suit)*10 + t.Num }

type MeldType int

const (
	MeldPung MeldType = iota
	MeldAnKong
	MeldMingKong
	MeldJiaKong
)

type Meld struct {
	Type  MeldType `json:"type"`
	Tiles []Tile   `json:"tiles"`
}

// FullDeck 136 tiles (wan/tong/tiao + winds + dragons)
func FullDeck() []Tile {
	var d []Tile
	for s := Wan; s <= Tiao; s++ {
		for n := 1; n <= 9; n++ {
			for i := 0; i < 4; i++ {
				d = append(d, Tile{Suit: s, Num: n})
			}
		}
	}
	for n := 1; n <= 4; n++ {
		for i := 0; i < 4; i++ {
			d = append(d, Tile{Suit: Feng, Num: n})
		}
	}
	for n := 1; n <= 3; n++ {
		for i := 0; i < 4; i++ {
			d = append(d, Tile{Suit: Jian, Num: n})
		}
	}
	return d
}