package zcash

import (
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/btcsuite/btcd/wire"
)

type Address struct {
	netaddr     *wire.NetAddress
	blacklisted bool
	lastUpdate  time.Time
}

func (a *Address) String() string {
	portString := strconv.Itoa(int(a.netaddr.Port))
	return net.JoinHostPort(a.netaddr.IP.String(), portString)
}

func (a *Address) asPeerKey() PeerKey {
	return PeerKey(a.String())
}

func (a *Address) fromPeerKey(s PeerKey) (*Address, error) {
	host, portString, err := net.SplitHostPort(s.String())
	if err != nil {
		return nil, err
	}

	portInt, err := strconv.ParseUint(portString, 10, 16)
	if err != nil {
		return nil, err
	}

	na := wire.NewNetAddressTimestamp(
		time.Now(),
		0,
		net.ParseIP(host),
		uint16(portInt),
	)

	a.netaddr = na
	a.blacklisted = false
	a.lastUpdate = na.Timestamp
	return a, nil
}

func (a *Address) asNetAddress() *wire.NetAddress {
	newNA := *a.netaddr
	newNA.Timestamp = a.lastUpdate
	return &newNA
}

func (a *Address) fromNetAddress(na *wire.NetAddress) (*Address, error) {
	a.netaddr = na
	a.blacklisted = false
	a.lastUpdate = na.Timestamp
	return a, nil
}

func (a *Address) MarshalText() (text []byte, err error) {
	return []byte(a.String()), nil
}

type AddressBook struct {
	addrs        map[PeerKey]*Address
	addrState    sync.RWMutex
	addrRecvCond *sync.Cond
}

func NewAddressBook() *AddressBook {
	addrBook := &AddressBook{
		addrs: make(map[PeerKey]*Address),
	}
	addrBook.addrRecvCond = sync.NewCond(&addrBook.addrState)
	return addrBook
}

func (bk *AddressBook) Add(s PeerKey) {
	newAddr, err := (&Address{}).fromPeerKey(s)
	if err != nil {
		// XXX effectively NOP bogus peer strings
		return
	}

	bk.addrState.Lock()
	bk.addrs[s] = newAddr
	bk.addrState.Unlock()

	// Wake anyone who was waiting on us to receive an address.
	bk.addrRecvCond.Broadcast()
}

func (bk *AddressBook) Remove(s PeerKey) {
	bk.addrState.Lock()
	defer bk.addrState.Unlock()

	if _, ok := bk.addrs[s]; ok {
		delete(bk.addrs, s)
	}
}

func (bk *AddressBook) Blacklist(s PeerKey) {
	bk.addrState.Lock()
	defer bk.addrState.Unlock()

	if target, ok := bk.addrs[s]; ok {
		target.blacklisted = true
		target.lastUpdate = time.Now()
	} else {
		// Create a new Address just to be blacklisted
		addr, err := (&Address{}).fromPeerKey(s)
		if err != nil {
			// XXX effectively NOP bogus peer strings
			return
		}
		addr.blacklisted = true
		bk.addrs[s] = addr
	}
}

// Touch updates the last-seen timestamp if the peer is in the address book or does nothing if not.
func (bk *AddressBook) Touch(s PeerKey) {
	bk.addrState.Lock()
	defer bk.addrState.Unlock()

	if target, ok := bk.addrs[s]; ok {
		target.lastUpdate = time.Now()
	}
}

// IsKnown returns true if the peer is already in our address book, false if not.
func (bk *AddressBook) IsKnown(s PeerKey) bool {
	bk.addrState.RLock()
	defer bk.addrState.RUnlock()

	_, known := bk.addrs[s]
	return known
}

func (bk *AddressBook) IsBlacklisted(s PeerKey) bool {
	bk.addrState.RLock()
	defer bk.addrState.RUnlock()

	if target, ok := bk.addrs[s]; ok {
		return target.blacklisted
	}

	return false
}

// WaitForAddresses waits for n addresses to be received and their initial
// connection attempts to resolve. There is no escape if that does not happen -
// this is intended for test runners or goroutines with a timeout.
func (bk *AddressBook) waitForAddresses(n int, done chan struct{}) {
	bk.addrState.Lock()
	for {
		addrCount := len(bk.addrs)
		if addrCount < n {
			bk.addrRecvCond.Wait()
		} else {
			break
		}
	}
	bk.addrState.Unlock()
	done <- struct{}{}
	return
}

// GetShuffledAddressList returns a slice of n valid addresses in random order.
// func (bk *AddressBook) GetShuffledAddressList(n int) []*Address {

// }
