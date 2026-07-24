package main

import (
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"mahjong/internal/game"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

type Msg struct {
	Type string          `json:"type"`
	Data json.RawMessage `json:"data,omitempty"`
}

type Player struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Conn *websocket.Conn `json:"-"`
}

type Room struct {
	ID      string    `json:"id"`
	Rule    string    `json:"rule"`
	Owner   string    `json:"owner"`
	InGame  bool      `json:"in_game"`
	Players []*Player `json:"players"`
	State   *State    `json:"-"`
}

type State struct {
	Rule      string
	Wall      []game.Tile
	Hands     [][]game.Tile
	Discards  [][]game.Tile
	Melds     [][]game.Meld
	Current   int
	Phase     string // exchange | dingque | discard | wait_action
	LastFrom  int
	LastTile  game.Tile
	Won       []bool
	Passed    []bool
	NeedAct   []bool
	Exchange  [][]game.Tile
	Exchanged []bool
	Que       []int  // 定缺花色 0万1筒2条，-1未选
	QueDone   []bool
}

type Hub struct {
	mu    sync.Mutex
	rooms map[string]*Room
}

var hub = &Hub{rooms: map[string]*Room{}}

func main() {
	rand.Seed(time.Now().UnixNano())
	http.HandleFunc("/ws", onWS)
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Write([]byte(`{"ok":true}`))
	})
	log.Println("Mahjong server :8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func onWS(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	p := &Player{ID: id6(), Conn: c, Name: "玩家"}
	reply(c, "connected", map[string]any{"id": p.ID})
	defer onLeave(p)
	for {
		_, b, err := c.ReadMessage()
		if err != nil {
			return
		}
		var m Msg
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		handle(p, m)
	}
}

func handle(p *Player, m Msg) {
	switch m.Type {
	case "set_name":
		var d struct {
			Name string `json:"name"`
		}
		json.Unmarshal(m.Data, &d)
		if d.Name != "" {
			p.Name = d.Name
		}
	case "create_room":
		createRoom(p, m.Data)
	case "join_room":
		joinRoom(p, m.Data)
	case "leave_room":
		leaveRoom(p)
	case "list_rooms":
		listRooms(p)
	case "start_game":
		startGame(p)
	case "discard":
		doDiscard(p, m.Data)
	case "action":
		doAction(p, m.Data)
	case "exchange":
		doExchange(p, m.Data)
	case "self_win":
		doSelfWin(p)
	case "dingque":
		doDingQue(p, m.Data)
	}
}

func createRoom(p *Player, raw json.RawMessage) {
	var d struct {
		Rule string `json:"rule"`
	}
	json.Unmarshal(raw, &d)
	if d.Rule != "sichuan" {
		d.Rule = "tuidaohu"
	}
	leaveRoom(p)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := &Room{ID: id4(), Rule: d.Rule, Owner: p.ID, Players: []*Player{p}}
	hub.rooms[r.ID] = r
	reply(p.Conn, "room", roomView(r))
}

func joinRoom(p *Player, raw json.RawMessage) {
	var d struct {
		RoomID string `json:"room_id"`
	}
	json.Unmarshal(raw, &d)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := hub.rooms[d.RoomID]
	if r == nil {
		reply(p.Conn, "error", map[string]string{"msg": "房间不存在"})
		return
	}
	if r.InGame {
		reply(p.Conn, "error", map[string]string{"msg": "已在对局中"})
		return
	}
	if len(r.Players) >= 4 {
		reply(p.Conn, "error", map[string]string{"msg": "房间已满"})
		return
	}
	for _, x := range r.Players {
		if x.ID == p.ID {
			reply(p.Conn, "room", roomView(r))
			return
		}
	}
	removeFromRoomsLocked(p)
	r.Players = append(r.Players, p)
	broadcast(r, "room", roomView(r))
}

func removeFromRoomsLocked(p *Player) {
	for id, r := range hub.rooms {
		for i, x := range r.Players {
			if x.ID == p.ID {
				r.Players = append(r.Players[:i], r.Players[i+1:]...)
				if len(r.Players) == 0 {
					delete(hub.rooms, id)
				} else {
					if r.Owner == p.ID {
						r.Owner = r.Players[0].ID
					}
					broadcast(r, "room", roomView(r))
				}
				return
			}
		}
	}
}

func leaveRoom(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	removeFromRoomsLocked(p)
}

func onLeave(p *Player) {
	leaveRoom(p)
	if p.Conn != nil {
		p.Conn.Close()
	}
}

func listRooms(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	type item struct {
		ID     string `json:"id"`
		Rule   string `json:"rule"`
		Count  int    `json:"count"`
		InGame bool   `json:"in_game"`
	}
	list := make([]item, 0)
	for _, r := range hub.rooms {
		list = append(list, item{ID: r.ID, Rule: r.Rule, Count: len(r.Players), InGame: r.InGame})
	}
	reply(p.Conn, "room_list", list)
}

func startGame(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.Owner != p.ID {
		return
	}
	if len(r.Players) < 2 {
		reply(p.Conn, "error", map[string]string{"msg": "至少 2 人"})
		return
	}
	n := len(r.Players)
	var deck []game.Tile
	if r.Rule == "sichuan" {
		deck = game.SichuanDeck()
	} else {
		deck = game.FullDeck()
	}
	game.Shuffle(deck)
	st := &State{
		Rule: r.Rule, Hands: make([][]game.Tile, n), Discards: make([][]game.Tile, n),
		Melds: make([][]game.Meld, n), Won: make([]bool, n),
		Exchange: make([][]game.Tile, n), Exchanged: make([]bool, n),
		Que: make([]int, n), QueDone: make([]bool, n),
	}
	for i := 0; i < n; i++ {
		st.Que[i] = -1
	}
	for i := 0; i < n; i++ {
		st.Hands[i] = append([]game.Tile{}, deck[:13]...)
		deck = deck[13:]
		game.SortHand(st.Hands[i])
	}
	st.Wall = deck
	r.State = st
	r.InGame = true
	if r.Rule == "sichuan" {
		st.Phase = "exchange"
		for i, pl := range r.Players {
			reply(pl.Conn, "game_start", map[string]any{
				"seat": i, "rule": r.Rule, "hand": st.Hands[i], "exchange": true, "players": names(r),
			})
		}
		return
	}
	st.Current = 0
	st.Phase = "discard"
	drawOne(st, 0)
	for i, pl := range r.Players {
		reply(pl.Conn, "game_start", map[string]any{
			"seat": i, "rule": r.Rule, "hand": st.Hands[i], "exchange": false, "players": names(r),
		})
	}
	broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": len(st.Wall)})
}

func doExchange(p *Player, raw json.RawMessage) {
	var wrap struct {
		Tiles json.RawMessage `json:"tiles"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		reply(p.Conn, "error", map[string]string{"msg": "数据格式错误"})
		return
	}
	tiles, err := game.ParseTiles(wrap.Tiles)
	if err != nil || len(tiles) != 3 {
		reply(p.Conn, "error", map[string]string{"msg": "必须选 3 张有效牌"})
		return
	}
	suit := tiles[0].Suit
	for _, t := range tiles {
		if t.Suit != suit || t.Suit > game.Tiao {
			reply(p.Conn, "error", map[string]string{"msg": "换三张须同花色"})
			return
		}
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil || r.State.Phase != "exchange" {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || st.Exchanged[seat] {
		return
	}
	hand := append([]game.Tile{}, st.Hands[seat]...)
	for _, t := range tiles {
		var ok bool
		hand, ok = game.RemoveOne(hand, t)
		if !ok {
			reply(p.Conn, "error", map[string]string{"msg": "手牌不足"})
			return
		}
	}
	st.Hands[seat] = hand
	st.Exchange[seat] = tiles
	st.Exchanged[seat] = true
	reply(p.Conn, "exchange_ok", map[string]any{"ok": true})
	for i := 0; i < len(r.Players); i++ {
		if !st.Exchanged[i] {
			return
		}
	}
	dice := rand.Intn(6) + rand.Intn(6) + 2
	dir := game.ExchangeDir(dice)
	n := len(r.Players)
	newHands := make([][]game.Tile, n)
	for i := 0; i < n; i++ {
		from := (i - dir + n*3) % n
		newHands[i] = append(st.Hands[i], st.Exchange[from]...)
		game.SortHand(newHands[i])
	}
	st.Hands = newHands
	// 记录每人新换入的 3 张供客户端高亮
	received := make([][]game.Tile, n)
	for i := 0; i < n; i++ {
		from := (i - dir + n*3) % n
		received[i] = append([]game.Tile{}, st.Exchange[from]...)
	}
	st.Phase = "dingque"
	for i, pl := range r.Players {
		reply(pl.Conn, "exchange_done", map[string]any{
			"hand": st.Hands[i], "direction": dir, "dice": dice,
			"received": received[i], "need_dingque": true,
		})
	}
}

func doDingQue(p *Player, raw json.RawMessage) {
	var d struct {
		Suit int `json:"suit"` // 0万 1筒 2条
	}
	if json.Unmarshal(raw, &d) != nil || d.Suit < 0 || d.Suit > 2 {
		reply(p.Conn, "error", map[string]string{"msg": "定缺须选万/筒/条"})
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil || r.State.Phase != "dingque" {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || st.QueDone[seat] {
		return
	}
	st.Que[seat] = d.Suit
	st.QueDone[seat] = true
	reply(p.Conn, "dingque_ok", map[string]any{"suit": d.Suit})
	broadcast(r, "dingque_update", map[string]any{"seat": seat, "suit": d.Suit})

	for i := 0; i < len(r.Players); i++ {
		if !st.QueDone[i] {
			return
		}
	}
	// 全部定缺完毕，庄家摸牌开始
	st.Phase = "discard"
	st.Current = 0
	drawOne(st, 0)
	ques := make([]int, len(r.Players))
	copy(ques, st.Que)
	for i, pl := range r.Players {
		reply(pl.Conn, "game_ready", map[string]any{
			"hand": st.Hands[i], "que": st.Que[i], "all_que": ques,
			"draw": st.Hands[i][len(st.Hands[i])-1],
		})
	}
	broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": len(st.Wall)})
}

func doDiscard(p *Player, raw json.RawMessage) {
	var wrap struct {
		Tile json.RawMessage `json:"tile"`
	}
	if json.Unmarshal(raw, &wrap) != nil {
		return
	}
	tile, err := game.ParseTile(wrap.Tile)
	if err != nil {
		reply(p.Conn, "error", map[string]string{"msg": "无效的牌"})
		return
	}
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || seat != st.Current || st.Phase != "discard" || st.Won[seat] {
		reply(p.Conn, "error", map[string]string{"msg": "还没轮到你出牌"})
		return
	}
	// 川麻：手中还有定缺花色时，必须先打定缺
	if st.Rule == "sichuan" && st.Que[seat] >= 0 {
		hasQue := false
		for _, h := range st.Hands[seat] {
			if int(h.Suit) == st.Que[seat] {
				hasQue = true
				break
			}
		}
		if hasQue && int(tile.Suit) != st.Que[seat] {
			reply(p.Conn, "error", map[string]string{"msg": "须先打出定缺花色的牌"})
			return
		}
	}
	hand, ok := game.RemoveOne(st.Hands[seat], tile)
	if !ok {
		reply(p.Conn, "error", map[string]string{"msg": "手牌中没有这张牌"})
		return
	}
	st.Hands[seat] = hand
	game.SortHand(st.Hands[seat])
	st.Discards[seat] = append(st.Discards[seat], tile)
	st.LastFrom = seat
	st.LastTile = tile
	st.Phase = "wait_action"
	st.Passed = make([]bool, len(r.Players))
	st.NeedAct = make([]bool, len(r.Players))
	broadcast(r, "discarded", map[string]any{"seat": seat, "tile": tile})
	reply(p.Conn, "hand", map[string]any{"hand": st.Hands[seat]})
	need := false
	for i := 0; i < len(r.Players); i++ {
		if i == seat || st.Won[i] {
			continue
		}
		cand := append(append([]game.Tile{}, st.Hands[i]...), tile)
		canP := game.CanPung(st.Hands[i], tile)
		// 推倒胡只允许自摸；四川可点炮且必须已定缺、手牌无定缺花色
		canW := false
		if st.Rule == "sichuan" && st.Que[i] >= 0 {
			canW = game.CanWinSichuan(cand, st.Melds[i], game.Suit(st.Que[i]))
		}
		// 四川：不能碰定缺花色
		if st.Rule == "sichuan" && st.Que[i] >= 0 && int(tile.Suit) == st.Que[i] {
			canP = false
		}
		if canP || canW {
			need = true
			st.NeedAct[i] = true
			reply(r.Players[i].Conn, "action_prompt", map[string]any{
				"tile": tile, "from": seat, "can_pung": canP, "can_win": canW, "self": false,
			})
		}
	}
	if !need {
		nextTurn(r, seat)
	}
}

func doAction(p *Player, raw json.RawMessage) {
	var d struct {
		Action string `json:"action"`
	}
	json.Unmarshal(raw, &d)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil || r.State.Phase != "wait_action" {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || st.Won[seat] || !st.NeedAct[seat] {
		return
	}
	switch d.Action {
	case "pung":
		if !game.CanPung(st.Hands[seat], st.LastTile) {
			return
		}
		hand, ok := game.RemoveN(st.Hands[seat], st.LastTile, 2)
		if !ok {
			return
		}
		st.Hands[seat] = hand
		t := st.LastTile
		st.Melds[seat] = append(st.Melds[seat], game.Meld{Type: game.MeldPung, Tiles: []game.Tile{t, t, t}})
		st.Current = seat
		st.Phase = "discard"
		if len(st.Discards[st.LastFrom]) > 0 {
			st.Discards[st.LastFrom] = st.Discards[st.LastFrom][:len(st.Discards[st.LastFrom])-1]
		}
		broadcast(r, "pung", map[string]any{"seat": seat, "tile": t, "from": st.LastFrom})
		reply(p.Conn, "hand", map[string]any{"hand": st.Hands[seat]})
		broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": len(st.Wall)})
	case "win":
		// 推倒胡禁止点炮，只能 self_win
		if st.Rule != "sichuan" {
			reply(p.Conn, "error", map[string]string{"msg": "推倒胡只能自摸"})
			return
		}
		if st.Que[seat] < 0 {
			reply(p.Conn, "error", map[string]string{"msg": "请先定缺"})
			return
		}
		cand := append(append([]game.Tile{}, st.Hands[seat]...), st.LastTile)
		if !game.CanWinSichuan(cand, st.Melds[seat], game.Suit(st.Que[seat])) {
			reply(p.Conn, "error", map[string]string{"msg": "未定缺干净或牌型不能胡"})
			return
		}
		st.Hands[seat] = cand
		st.Won[seat] = true
		broadcast(r, "won", map[string]any{"seat": seat, "from": st.LastFrom, "hand": st.Hands[seat], "continue": st.Rule == "sichuan"})
		if st.Rule == "sichuan" {
			alive := 0
			for i := range st.Won {
				if !st.Won[i] {
					alive++
				}
			}
			if alive <= 1 {
				endGame(r)
			} else {
				nextTurn(r, st.LastFrom)
			}
		} else {
			endGame(r)
		}
	case "pass":
		st.Passed[seat] = true
		st.NeedAct[seat] = false
		for i := range st.NeedAct {
			if st.NeedAct[i] && !st.Passed[i] {
				return
			}
		}
		nextTurn(r, st.LastFrom)
	}
}

func doSelfWin(p *Player) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	r := findRoom(p)
	if r == nil || r.State == nil {
		return
	}
	st := r.State
	seat := seatOf(r, p)
	if seat < 0 || seat != st.Current || st.Phase != "discard" || st.Won[seat] {
		return
	}
	if st.Rule == "sichuan" {
		if st.Que[seat] < 0 {
			reply(p.Conn, "error", map[string]string{"msg": "请先定缺"})
			return
		}
		if !game.CanWinSichuan(st.Hands[seat], st.Melds[seat], game.Suit(st.Que[seat])) {
			reply(p.Conn, "error", map[string]string{"msg": "未定缺干净或牌型不能胡"})
			return
		}
	} else if !game.CanWin(st.Hands[seat]) {
		return
	}
	st.Won[seat] = true
	broadcast(r, "won", map[string]any{"seat": seat, "from": -1, "hand": st.Hands[seat], "continue": st.Rule == "sichuan"})
	if st.Rule == "sichuan" {
		alive := 0
		for i := range st.Won {
			if !st.Won[i] {
				alive++
			}
		}
		if alive <= 1 {
			endGame(r)
		} else {
			nextTurn(r, seat)
		}
	} else {
		endGame(r)
	}
}

func nextTurn(r *Room, after int) {
	st := r.State
	n := len(r.Players)
	for k := 1; k <= n; k++ {
		i := (after + k) % n
		if st.Won[i] {
			continue
		}
		if len(st.Wall) == 0 {
			endGame(r)
			return
		}
		drawOne(st, i)
		st.Current = i
		st.Phase = "discard"
		canSelf := false
		if st.Rule == "sichuan" {
			if st.Que[i] >= 0 {
				canSelf = game.CanWinSichuan(st.Hands[i], st.Melds[i], game.Suit(st.Que[i]))
			}
		} else {
			canSelf = game.CanWin(st.Hands[i])
		}
		if canSelf {
			reply(r.Players[i].Conn, "action_prompt", map[string]any{
				"tile": st.Hands[i][len(st.Hands[i])-1], "from": -1, "can_pung": false, "can_win": true, "self": true,
			})
		}
		reply(r.Players[i].Conn, "draw", map[string]any{"tile": st.Hands[i][len(st.Hands[i])-1], "hand": st.Hands[i]})
		broadcast(r, "turn", map[string]any{"current": st.Current, "phase": st.Phase, "wall": len(st.Wall)})
		return
	}
	endGame(r)
}

func drawOne(st *State, seat int) {
	if len(st.Wall) == 0 {
		return
	}
	st.Hands[seat] = append(st.Hands[seat], st.Wall[0])
	st.Wall = st.Wall[1:]
}

func endGame(r *Room) {
	st := r.State
	type row struct {
		Name string      `json:"name"`
		Won  bool        `json:"won"`
		Hand []game.Tile `json:"hand"`
	}
	var res []row
	for i, pl := range r.Players {
		res = append(res, row{Name: pl.Name, Won: st.Won[i], Hand: st.Hands[i]})
	}
	broadcast(r, "game_over", map[string]any{"result": res})
	r.InGame = false
	r.State = nil
	broadcast(r, "room", roomView(r))
}

func findRoom(p *Player) *Room {
	for _, r := range hub.rooms {
		for _, x := range r.Players {
			if x.ID == p.ID {
				return r
			}
		}
	}
	return nil
}

func seatOf(r *Room, p *Player) int {
	for i, x := range r.Players {
		if x.ID == p.ID {
			return i
		}
	}
	return -1
}

func names(r *Room) []string {
	out := make([]string, len(r.Players))
	for i, p := range r.Players {
		out[i] = p.Name
	}
	return out
}

func roomView(r *Room) map[string]any {
	ps := make([]map[string]string, 0, len(r.Players))
	for _, p := range r.Players {
		ps = append(ps, map[string]string{"id": p.ID, "name": p.Name})
	}
	return map[string]any{"id": r.ID, "rule": r.Rule, "owner": r.Owner, "in_game": r.InGame, "players": ps}
}

func reply(c *websocket.Conn, typ string, data any) {
	if c == nil {
		return
	}
	b, _ := json.Marshal(data)
	c.WriteJSON(Msg{Type: typ, Data: b})
}

func broadcast(r *Room, typ string, data any) {
	for _, p := range r.Players {
		reply(p.Conn, typ, data)
	}
}

func id6() string {
	const c = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, 6)
	for i := range b {
		b[i] = c[rand.Intn(len(c))]
	}
	return string(b)
}

func id4() string { return id6()[:4] }
