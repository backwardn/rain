package piecedownloader

import (
	"crypto/sha1" // nolint: gosec
	"errors"
	"fmt"
	"io"

	"github.com/cenkalti/rain/torrent/internal/peer"
	"github.com/cenkalti/rain/torrent/internal/peerprotocol"
	"github.com/cenkalti/rain/torrent/internal/pieceio"
)

const maxQueuedBlocks = 10

// PieceDownloader downloads all blocks of a piece from a peer.
type PieceDownloader struct {
	Piece          *pieceio.Piece
	Peer           *peer.Peer
	buffer         []byte
	nextBlockIndex uint32
	requested      map[uint32]struct{}
	done           map[uint32]struct{}
	PieceC         chan Piece
	RejectC        chan *pieceio.Block
	ChokeC         chan struct{}
	UnchokeC       chan struct{}
	resultC        chan Result
	closeC         chan struct{}
	doneC          chan struct{}
}

type Result struct {
	Peer  *peer.Peer
	Piece *pieceio.Piece
	Error error
}

func New(pi *pieceio.Piece, pe *peer.Peer, resultC chan Result) *PieceDownloader {
	return &PieceDownloader{
		Piece:     pi,
		Peer:      pe,
		buffer:    make([]byte, pi.Length),
		requested: make(map[uint32]struct{}),
		done:      make(map[uint32]struct{}),
		PieceC:    make(chan Piece),
		RejectC:   make(chan *pieceio.Block),
		ChokeC:    make(chan struct{}),
		UnchokeC:  make(chan struct{}),
		resultC:   resultC,
		closeC:    make(chan struct{}),
		doneC:     make(chan struct{}),
	}
}

func (d *PieceDownloader) Close() {
	close(d.closeC)
}

func (d *PieceDownloader) Done() <-chan struct{} {
	return d.doneC
}

func (d *PieceDownloader) requestBlocks() {
	for ; d.nextBlockIndex < uint32(len(d.Piece.Blocks)) && len(d.requested) < maxQueuedBlocks; d.nextBlockIndex++ {
		b := d.Piece.Blocks[d.nextBlockIndex]
		if _, ok := d.done[d.nextBlockIndex]; ok {
			continue
		}
		d.requested[d.nextBlockIndex] = struct{}{}
		msg := peerprotocol.RequestMessage{Index: d.Piece.Index, Begin: b.Begin, Length: b.Length}
		d.Peer.SendMessage(msg)
	}
}

func (d *PieceDownloader) Run() {
	defer close(d.doneC)

	result := Result{
		Peer:  d.Peer,
		Piece: d.Piece,
	}
	defer func() {
		select {
		case d.resultC <- result:
		case <-d.closeC:
		}
	}()

	d.requestBlocks()
	for {
		select {
		case p := <-d.PieceC:
			if _, ok := d.requested[p.Block.Index]; !ok {
				result.Error = fmt.Errorf("peer sent unrequested piece block: %q", p.Block)
				return
			}
			delete(d.requested, p.Block.Index)
			_, result.Error = io.ReadFull(p.Piece.Reader, d.buffer[p.Piece.Begin:p.Piece.Begin+p.Piece.Length])
			close(p.Piece.Done)
			if result.Error != nil {
				return
			}
			d.done[p.Block.Index] = struct{}{}
			if d.allDone() {
				ok := d.Piece.VerifyHash(d.buffer, sha1.New()) // nolint: gosec
				if !ok {
					result.Error = errors.New("received corrupt piece")
					break
				}
				_, result.Error = d.Piece.Data.Write(d.buffer)
				return
			}
			d.requestBlocks()
		case blk := <-d.RejectC:
			if _, ok := d.requested[blk.Index]; !ok {
				result.Error = fmt.Errorf("peer sent reject to unrequested block: %q", blk)
				return
			}
			delete(d.requested, blk.Index)
			d.nextBlockIndex = 0
		case <-d.ChokeC:
			d.requested = make(map[uint32]struct{})
			d.nextBlockIndex = 0
		case <-d.UnchokeC:
			d.requestBlocks()
		case <-d.closeC:
			return
		}
	}
}

func (d *PieceDownloader) allDone() bool {
	return len(d.done) == len(d.Piece.Blocks)
}
