package game

// Suit: 0万 1筒 2条 3风 4箭
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
	Num  int  `json:"num"` // 1-9 / 风1-4 / 箭1-3
}

func (t Tile) ID() int { return int(t.Suit)*10 + t.Num }

type MeldType int

const (
	MeldPung MeldType = iota
	MeldKong
)

type Meld struct {
	Type  MeldType `json:"type"`
	Tiles []Tile   `json:"tiles"`
}

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

// 四川麻将：只有万筒条 108 张
func SichuanDeck() []Tile {
	var d []Tile
	for s := Wan; s <= Tiao; s++ {
		for n := 1; n <= 9; n++ {
			for i := 0; i < 4; i++ {
				d = append(d, Tile{Suit: s, Num: n})
			}
		}
	}
	return d
}

// 换三张方向：1下家 2对家 3上家
func ExchangeDir(diceSum int) int {
	switch diceSum % 3 {
	case 1:
		return 1
	case 2:
		return 3
	default:
		return 2
	}
}

// 缺一门：手牌+副露最多出现两种花色（万筒条）
func HasQueYiMen(hand []Tile, melds []Meld) bool {
	seen := map[Suit]bool{}
	for _, t := range hand {
		if t.Suit <= Tiao {
			seen[t.Suit] = true
		}
	}
	for _, m := range melds {
		for _, t := range m.Tiles {
			if t.Suit <= Tiao {
				seen[t.Suit] = true
			}
		}
	}
	return len(seen) <= 2
}
