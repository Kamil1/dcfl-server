package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

type hub struct {
	// Connections mutex.
	connectionsMx sync.RWMutex

	// Registered connections.
	connections map[*connection]struct{}

	// Side mutex.
	sideMx sync.Mutex

	// Registered black players.
	blackSide [2]player

	blackSideTeamDisplayName string

	// Registered yellow players.
	yellowSide [2]player

	yellowSideTeamDisplayName string

	// Score mutex.
	scoreMx sync.Mutex

	blackScore int

	yellowScore int

	gameStarted bool

	gameOver bool

	// Inbound request messages from the connections.
	requests chan []byte

	// Outbound messages from the server.
	confirmations chan string

	logMx sync.RWMutex
	log   [][]byte
}

type dcflMsg struct {
	// the action performed by the server
	Action string `json:"action"`
	// the id of the user the action was performed against, if applicable
	Sub string `json:"sub"`
	// the side the action was performed against, if applicable
	Side string `json:"side"`
	// picture related to the action, if applicable
	Picture string `json:"picture"`
	// player1, if applicable
	Player1 string `json:"player_1"`
	// player2, if applicable
	Player2 string `json:"player_2"`
	// city, if applicable
	City string `json:"city"`
	// name, if applicable
	Name string `json:"name"`
}

type matchState struct {
	BlackPlayer1  player `json:"black_player_1"`
	BlackPlayer2  player `json:"black_player_2"`
	YellowPlayer1 player `json:"yellow_player_1"`
	YellowPlayer2 player `json:"yellow_player_2"`
	GameStarted   bool   `json:"game_started"`
	GameOver      bool   `json:"game_over"`
	BlackScore    int    `json:"black_score"`
	YellowScore   int    `json:"yellow_score"`
	Error         string `json:"error"`
}

type player struct {
	Sub       string `json:"sub"`
	Picture   string `json:"picture"`
	Confirmed bool   `json:"confirmed"`
}

func startGame(h *hub) {
	// TODO
}

func resetError(h *hub, errorMsg string) {
	h.confirmations <- errorMsg
	reset := dcflMsg{Action: "reset"}
	resetJSON, _ := json.Marshal(reset)
	h.requests <- resetJSON
}

func unregister(h *hub, sub string, side string) error {
	unregister := dcflMsg{Action: "unregister", Sub: sub, Side: side}
	unregisterJSON, err := json.Marshal(unregister)
	if err != nil {
		return err
	}
	h.requests <- unregisterJSON
	return nil
}

func getTeamName(player1 string, player2 string) (string, error) {
	var city string
	var name string
	err := db.QueryRow(
		"SELECT city, name FROM public.team WHERE (player1 = $1 AND player2 = $2) OR (player1 = $2 AND player2 = $1)",
		player1,
		player2,
	).Scan(&city, &name)
	if err == sql.ErrNoRows {
		// If client sees that both players have confirmed, but no team name exists,
		// it must prompt user to register team.
		return "", nil
	} else if err != nil {
		return "", err
	}

	return city + " " + name, nil
}

func newHub() *hub {
	h := &hub{
		connectionsMx: sync.RWMutex{},
		requests:      make(chan []byte, 1),
		confirmations: make(chan string),
		sideMx:        sync.Mutex{},
		blackSideTeamDisplayName:  "",
		yellowSideTeamDisplayName: "",
		scoreMx:                   sync.Mutex{},
		blackScore:                0,
		yellowScore:               0,
		gameStarted:               false,
		gameOver:                  false,
		connections:               make(map[*connection]struct{}),
	}

	go func() {
		for {
			fmt.Println("polling...")
			msg := <-h.requests

			cm := &dcflMsg{}
			err := json.Unmarshal(msg, cm)
			if err != nil {
				continue
			}

			switch cm.Action {
			case "register game":
				h.sideMx.Lock()
				fmt.Println("Registering")
				if cm.Side == "black" {
					// Check if black side is full.
					if h.blackSide[0] != (player{}) && h.blackSide[1] != (player{}) {
						fmt.Println("Black side full")
						h.sideMx.Unlock()
						break
					}

					// Check if already registered on black side.
					found := false
					for _, v := range h.blackSide {
						if v.Sub == cm.Sub {
							found = true
						}
					}
					if found {
						fmt.Println("Already registered to black side, unregistering")
						err = unregister(h, cm.Sub, "black")
						if err != nil {
							fmt.Println(err)
						}
						h.sideMx.Unlock()
						break
					}
				} else if cm.Side == "yellow" {
					// Check if yellow side is full.
					if h.yellowSide[0] != (player{}) && h.yellowSide[1] != (player{}) {
						fmt.Println("Yellow side full")
						h.sideMx.Unlock()
						break
					}

					// Check if already registered on yellow side.
					found := false
					for _, v := range h.yellowSide {
						if v.Sub == cm.Sub {
							found = true
						}
					}
					if found {
						fmt.Println("Already registered to yellow side, unregistering")
						err = unregister(h, cm.Sub, "yellow")
						if err != nil {
							fmt.Println(err)
						}
						h.sideMx.Unlock()
						break
					}
				} else {
					// If side is neither black nor yellow, ignore the request.
					h.sideMx.Unlock()
					break
				}

				fmt.Println("Getting user picture")
				var picture string
				err = db.QueryRow("SELECT picture FROM public.player WHERE id = $1", cm.Sub).Scan(&picture)
				if err != nil {
					break
				}

				if cm.Side == "black" {
					// If registering for black side, unregister from yellow side.
					fmt.Println("Registering to black side")
					var err error
					for _, v := range h.yellowSide {
						if v.Sub == cm.Sub {
							fmt.Println("Unregistering from yellow side")
							err = unregister(h, cm.Sub, "yellow")
						}
					}
					if err != nil {
						h.sideMx.Unlock()
						break
					}

					// Register to first free side slot.
					fmt.Println("Placing in black player slot")
					if h.blackSide[0] == (player{}) {
						fmt.Println("Placed in black slot 1")
						h.blackSide[0] = player{Sub: cm.Sub, Picture: picture, Confirmed: false}
					} else if h.blackSide[1] == (player{}) {
						fmt.Println("Placed in black slot 2")
						h.blackSide[1] = player{Sub: cm.Sub, Picture: picture, Confirmed: false}
					}
				} else {
					// If registering for yellow side, unregister from black side.
					fmt.Println("Registering to yellow side")
					var err error
					for _, v := range h.blackSide {
						if v.Sub == cm.Sub {
							fmt.Println("Unregistering from black side")
							err = unregister(h, cm.Sub, "black")
						}
					}
					if err != nil {
						h.sideMx.Unlock()
						break
					}

					// Register to first free side slot.
					fmt.Println("Placing in yellow player slot")
					if h.yellowSide[0] == (player{}) {
						fmt.Println("Placed in yellow slot 1")
						h.yellowSide[0] = player{Sub: cm.Sub, Picture: picture, Confirmed: false}
					} else if h.yellowSide[1] == (player{}) {
						fmt.Println("Placed in yellow slot 2")
						h.yellowSide[1] = player{Sub: cm.Sub, Picture: picture, Confirmed: false}
					}
				}

				// Broadcast current match state to clients.
				h.confirmations <- "match state"
				h.sideMx.Unlock()
			case "unregister":
				fmt.Println("Unregistering")
				h.sideMx.Lock()
				if cm.Side == "black" {
					if h.blackSide[0].Sub == cm.Sub {
						h.blackSide[0] = player{}
					} else if h.blackSide[1].Sub == cm.Sub {
						h.blackSide[1] = player{}
					}
				} else if cm.Side == "yellow" {
					if h.yellowSide[0].Sub == cm.Sub {
						h.yellowSide[0] = player{}
					} else if h.yellowSide[1].Sub == cm.Sub {
						h.yellowSide[1] = player{}
					}
				} else {
					h.sideMx.Unlock()
					break
				}

				h.confirmations <- "match state"
				h.sideMx.Unlock()
			case "confirm":
				fmt.Println("Confirming")
				h.sideMx.Lock()
				if cm.Side == "black" {
					if h.blackSide[0].Sub == cm.Sub {
						fmt.Println("Confirming black player 1")
						h.blackSide[0].Confirmed = true
					} else if h.blackSide[1].Sub == cm.Sub {
						fmt.Println("Confirming black player 2")
						h.blackSide[1].Confirmed = true
					} else {
						fmt.Println("Black player not found")
						h.sideMx.Unlock()
						break
					}

					// Get team name.
					if h.blackSide[0].Confirmed && h.blackSide[1].Confirmed {
						name, err := getTeamName(h.blackSide[0].Sub, h.blackSide[1].Sub)
						if err != nil {
							fmt.Println(err)
							h.sideMx.Unlock()
							break
						}
						h.blackSideTeamDisplayName = name
					}

				} else if cm.Side == "yellow" {
					if h.yellowSide[0].Sub == cm.Sub {
						fmt.Println("Confirming yellow player 1")
						h.yellowSide[0].Confirmed = true
					} else if h.yellowSide[1].Sub == cm.Sub {
						fmt.Println("Confirming yellow player 2")
						h.yellowSide[1].Confirmed = true
					} else {
						fmt.Println("Yellow player not found")
						h.sideMx.Unlock()
						break
					}

					// Get team name.
					if h.yellowSide[0].Confirmed && h.yellowSide[1].Confirmed {
						name, err := getTeamName(h.yellowSide[0].Sub, h.yellowSide[1].Sub)
						if err != nil {
							fmt.Println(err)
							h.sideMx.Unlock()
							break
						}
						h.yellowSideTeamDisplayName = name
					}
				}
				if h.blackSide[0].Confirmed &&
					h.blackSide[1].Confirmed &&
					h.yellowSide[0].Confirmed &&
					h.yellowSide[1].Confirmed &&
					h.blackSideTeamDisplayName != "" &&
					h.yellowSideTeamDisplayName != "" {
					startGame(h)
				}
				h.confirmations <- "match state"
				h.sideMx.Unlock()
			case "register team":
				fmt.Println("Registering team")
				if cm.Player1 == "" ||
					cm.Player2 == "" ||
					cm.Player1 == cm.Player2 ||
					cm.City == "" ||
					cm.Name == "" {
					resetError(h, "Error registering teams")
					break
				}
				var count int
				err := db.QueryRow(
					"SELECT COUNT(*) FROM public.team WHERE (player1 = $1 AND player2 = $2) OR (player1 = $2 AND player2 = $1) OR city = $3 OR name = $4",
					cm.Player1,
					cm.Player2,
					cm.City,
					cm.Name).Scan(&count)
				if err != nil || count != 0 {
					resetError(h, "Error registering teams")
					break
				}

				_, err = db.Exec(
					"INSERT INTO public.team(city, name, player1, player2) VALUES ($1, $2, $3, $4)",
					cm.City,
					cm.Name,
					cm.Player1,
					cm.Player2)

				if cm.Side == "black" {
					h.blackSideTeamDisplayName = cm.City + " " + cm.Name
				} else if cm.Side == "yellow" {
					h.yellowSideTeamDisplayName = cm.City + " " + cm.Name
				} else {
					resetError(h, "Error setting up team")
				}
				h.confirmations <- "match state"
			}
		}
	}()

	go func() {
		for {
			var msg []byte
			confirmation := <-h.confirmations
			fmt.Println("Sending confirmations")

			switch confirmation {
			case "match state":
				state := matchState{
					BlackPlayer1:  h.blackSide[0],
					BlackPlayer2:  h.blackSide[1],
					YellowPlayer1: h.yellowSide[0],
					YellowPlayer2: h.yellowSide[1],
					BlackScore:    h.blackScore,
					YellowScore:   h.yellowScore,
					GameStarted:   h.gameStarted,
					GameOver:      h.gameOver,
				}
				stateJSON, _ := json.Marshal(state)
				msg = stateJSON
			default:
				state := matchState{Error: confirmation}
				stateJSON, _ := json.Marshal(state)
				msg = stateJSON
			}

			h.connectionsMx.RLock()
			for c := range h.connections {
				select {
				case c.send <- msg:
				// stop trying to send to this connection after trying for 1 second.
				// if we have to stop, it means that a reader died so remove the connection also.
				case <-time.After(1 * time.Second):
					h.removeConnection(c)
				}
			}
			h.connectionsMx.RUnlock()
		}
	}()

	return h
}

func (h *hub) addConnection(conn *connection) {
	h.connectionsMx.Lock()
	defer h.connectionsMx.Unlock()
	if len(h.connections) < 4 {
		h.connections[conn] = struct{}{}
		h.confirmations <- "match state"
	} else {
		state := matchState{Error: "Lobby full"}
		stateJSON, _ := json.Marshal(state)
		conn.send <- stateJSON
		close(conn.send)
	}
}

func (h *hub) removeConnection(conn *connection) {
	h.connectionsMx.Lock()
	defer h.connectionsMx.Unlock()
	if _, ok := h.connections[conn]; ok {
		unregister(h, conn.sub, "black")
		unregister(h, conn.sub, "yellow")
		delete(h.connections, conn)
		close(conn.send)
	}
}
