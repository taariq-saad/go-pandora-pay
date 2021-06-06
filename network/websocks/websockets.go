package websocks

import (
	"encoding/json"
	"errors"
	"nhooyr.io/websocket"
	"pandora-pay/blockchain"
	"pandora-pay/config"
	"pandora-pay/config/globals"
	"pandora-pay/gui"
	"pandora-pay/helpers/multicast"
	api_http "pandora-pay/network/api/api-http"
	"pandora-pay/network/api/api-websockets"
	"pandora-pay/network/websocks/connection"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

type Websockets struct {
	AllAddresses                 *sync.Map
	AllList                      *atomic.Value //[]*connection.AdvancedConnection
	AllListMutex                 *sync.Mutex
	Clients                      int64
	ServerSockets                int64
	TotalSockets                 int64
	UpdateNewConnectionMulticast *multicast.MulticastChannel
	UpdateAccounts               *multicast.MulticastChannel
	UpdateTokens                 *multicast.MulticastChannel
	ApiWebsockets                *api_websockets.APIWebsockets
	subscriptions                *WebsocketSubscriptions
	api                          *api_http.API
}

func (websockets *Websockets) GetFirstSocket() *connection.AdvancedConnection {
	list := websockets.AllList.Load().([]*connection.AdvancedConnection)
	if len(list) > 0 {
		return list[0]
	}
	return nil
}

func (websockets *Websockets) GetAllSockets() []*connection.AdvancedConnection {
	return websockets.AllList.Load().([]*connection.AdvancedConnection)
}

func (websockets *Websockets) Broadcast(name []byte, data []byte, consensusTypeAccepted map[config.ConsensusType]bool) {

	all := websockets.GetAllSockets()
	for _, conn := range all {
		if consensusTypeAccepted[conn.Handshake.Consensus] {
			conn.Send(name, data)
		}
	}

}

func (websockets *Websockets) BroadcastJSON(name []byte, data interface{}, consensusTypeAccepted map[config.ConsensusType]bool) {
	out, _ := json.Marshal(data)
	websockets.Broadcast(name, out, consensusTypeAccepted)
}

func (websockets *Websockets) closedConnection(conn *connection.AdvancedConnection) {

	<-conn.Closed

	conn2, exists := websockets.AllAddresses.LoadAndDelete(conn.RemoteAddr)
	if !exists || conn2 != conn {
		return
	}

	if !conn.Initialized {
		return
	}

	websockets.AllListMutex.Lock()
	all := websockets.AllList.Load().([]*connection.AdvancedConnection)
	for i, conn2 := range all {
		if conn2 == conn {
			//order is not important
			all[i] = all[len(all)-1]
			all = all[:len(all)-1]
			websockets.AllList.Store(all)
			break
		}
	}
	websockets.AllListMutex.Unlock()

	websockets.subscriptions.websocketClosedCn <- conn

	if conn.ConnectionType {
		atomic.AddInt64(&websockets.ServerSockets, -1)
	} else {
		atomic.AddInt64(&websockets.Clients, -1)
	}
	totalSockets := atomic.AddInt64(&websockets.TotalSockets, -1)
	globals.MainEvents.BroadcastEvent("sockets/totalSocketsChanged", totalSockets)
}

func (websockets *Websockets) NewConnection(c *websocket.Conn, addr string, connectionType bool) (conn *connection.AdvancedConnection, err error) {

	if conn, err = connection.CreateAdvancedConnection(c, addr, websockets.ApiWebsockets.GetMap, connectionType, websockets.subscriptions.newSubscriptionCn, websockets.subscriptions.removeSubscriptionCn); err != nil {
		return
	}

	if _, exists := websockets.AllAddresses.LoadOrStore(addr, conn); exists {
		return nil, errors.New("Already connected")
	}

	go conn.ReadPump()
	go conn.WritePump()
	go websockets.closedConnection(conn)

	if err = websockets.InitializeConnection(conn); err != nil {
		return
	}

	return
}

func (websockets *Websockets) InitializeConnection(conn *connection.AdvancedConnection) error {

	out := conn.SendAwaitAnswer([]byte("handshake"), nil)

	if out.Err != nil {
		conn.Close("Error sending handshake")
		return nil
	}
	if out.Out == nil {
		conn.Close("Handshake was not received")
		return errors.New("Handshake was not received")
	}

	handshakeReceived := new(connection.ConnectionHandshake)
	if err := json.Unmarshal(out.Out, &handshakeReceived); err != nil {
		conn.Close("Handshake received was invalid")
		return errors.New("Handshake received was invalid")
	}

	if err := handshakeReceived.ValidateHandshake(); err != nil {
		conn.Close("Handshake is invalid")
		return errors.New("Handshake is invalid")
	}

	conn.Handshake = handshakeReceived

	websockets.AllListMutex.Lock()
	websockets.AllList.Store(append(websockets.AllList.Load().([]*connection.AdvancedConnection), conn))
	websockets.AllListMutex.Unlock()

	if conn.ConnectionType {
		atomic.AddInt64(&websockets.ServerSockets, +1)
	} else {
		atomic.AddInt64(&websockets.Clients, +1)
	}
	totalSockets := atomic.AddInt64(&websockets.TotalSockets, +1)
	globals.MainEvents.BroadcastEvent("sockets/totalSocketsChanged", totalSockets)

	conn.Initialized = true

	websockets.UpdateNewConnectionMulticast.Broadcast(conn)

	return nil
}

func CreateWebsockets(chain *blockchain.Blockchain, api *api_http.API, apiWebsockets *api_websockets.APIWebsockets) *Websockets {

	websockets := &Websockets{
		AllAddresses:                 &sync.Map{},
		Clients:                      0,
		ServerSockets:                0,
		AllList:                      &atomic.Value{},
		AllListMutex:                 &sync.Mutex{},
		UpdateNewConnectionMulticast: multicast.NewMulticastChannel(),
		api:                          api,
		ApiWebsockets:                apiWebsockets,
	}

	websockets.subscriptions = newWebsocketSubscriptions(websockets, chain)

	websockets.AllList.Store([]*connection.AdvancedConnection{})

	go func() {
		for {
			gui.GUI.InfoUpdate("sockets", strconv.FormatInt(atomic.LoadInt64(&websockets.Clients), 32)+" "+strconv.FormatInt(atomic.LoadInt64(&websockets.ServerSockets), 32))
			time.Sleep(1 * time.Second)
		}
	}()

	return websockets
}
