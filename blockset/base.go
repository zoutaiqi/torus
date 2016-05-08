package blockset

import (
	"sync/atomic"

	"golang.org/x/net/context"

	"github.com/RoaringBitmap/roaring"
	"github.com/coreos/agro"
	"github.com/coreos/pkg/capnslog"
)

type baseBlockset struct {
	ids       uint64
	blocks    []agro.BlockRef
	store     agro.BlockStore
	blocksize uint64
}

var _ blockset = &baseBlockset{}

func init() {
	RegisterBlockset(Base, func(_ string, store agro.BlockStore, _ blockset) (blockset, error) {
		return newBaseBlockset(store), nil
	})
}

func newBaseBlockset(store agro.BlockStore) *baseBlockset {
	b := &baseBlockset{
		blocks: make([]agro.BlockRef, 0),
		store:  store,
	}
	if store != nil {
		b.blocksize = store.BlockSize()
	}
	return b
}

func (b *baseBlockset) Length() int {
	return len(b.blocks)
}

func (b *baseBlockset) Kind() uint32 {
	return uint32(Base)
}

var zeroes = make([]byte, 1024*1024)

func (b *baseBlockset) GetBlock(ctx context.Context, i int) ([]byte, error) {
	if i >= len(b.blocks) {
		return nil, agro.ErrBlockNotExist
	}
	if b.blocks[i].IsZero() {
		for i := uint64(0); i < b.blocksize; i++ {
			zeroes[i] = 0
		}
		return zeroes[:b.blocksize], nil
	}
	clog.Tracef("base: getting block %d at BlockID %s", i, b.blocks[i])
	bytes, err := b.store.GetBlock(ctx, b.blocks[i])
	if err != nil {
		promBaseFail.Inc()
		return nil, err
	}
	return bytes, err
}

func (b *baseBlockset) PutBlock(ctx context.Context, inode agro.INodeRef, i int, data []byte) error {
	if i > len(b.blocks) {
		return agro.ErrBlockNotExist
	}
	// if v, ok := ctx.Value("isEmpty").(bool); ok && v {
	// 	clog.Debug("copying empty block")
	// 	if i == len(b.blocks) {
	// 		b.blocks = append(b.blocks, agro.ZeroBlock())
	// 	} else {
	// 		b.blocks[i] = agro.ZeroBlock()
	// 	}
	// 	return nil
	// }
	newBlockID := b.makeID(inode)
	if clog.LevelAt(capnslog.TRACE) {
		clog.Tracef("base: writing block %d at BlockID %s", i, newBlockID)
	}
	err := b.store.WriteBlock(ctx, newBlockID, data)
	if err != nil {
		return err
	}
	if i == len(b.blocks) {
		b.blocks = append(b.blocks, newBlockID)
	} else {
		b.blocks[i] = newBlockID
	}
	return nil
}

func (b *baseBlockset) makeID(i agro.INodeRef) agro.BlockRef {
	id := atomic.AddUint64(&b.ids, 1)
	return agro.BlockRef{
		INodeRef: i,
		Index:    agro.IndexID(id),
	}
}

func (b *baseBlockset) Marshal() ([]byte, error) {
	buf := make([]byte, len(b.blocks)*agro.BlockRefByteSize)
	for i, x := range b.blocks {
		x.ToBytesBuf(buf[(i * agro.BlockRefByteSize) : (i+1)*agro.BlockRefByteSize])
	}
	return buf, nil
}

func (b *baseBlockset) setStore(s agro.BlockStore) {
	b.blocksize = s.BlockSize()
	b.store = s
}

func (b *baseBlockset) getStore() agro.BlockStore {
	return b.store
}

func (b *baseBlockset) Unmarshal(data []byte) error {
	l := len(data) / agro.BlockRefByteSize
	out := make([]agro.BlockRef, l)
	for i := 0; i < l; i++ {
		out[i] = agro.BlockRefFromBytes(data[(i * agro.BlockRefByteSize) : (i+1)*agro.BlockRefByteSize])
	}
	b.blocks = out
	return nil
}

func (b *baseBlockset) GetSubBlockset() agro.Blockset { return nil }

func (b *baseBlockset) GetLiveINodes() *roaring.Bitmap {
	out := roaring.NewBitmap()
	for _, blk := range b.blocks {
		if blk.IsZero() {
			continue
		}
		out.Add(uint32(blk.INode))
	}
	return out
}

func (b *baseBlockset) Truncate(lastIndex int, _ uint64) error {
	if lastIndex <= len(b.blocks) {
		b.blocks = b.blocks[:lastIndex]
		return nil
	}
	toadd := lastIndex - len(b.blocks)
	for toadd != 0 {
		b.blocks = append(b.blocks, agro.ZeroBlock())
		toadd--
	}
	return nil
}

func (b *baseBlockset) Trim(from, to int) error {
	if from >= len(b.blocks) {
		return nil
	}
	if to > len(b.blocks) {
		to = len(b.blocks)
	}
	for i := from; i < to; i++ {
		b.blocks[i] = agro.ZeroBlock()
	}
	return nil
}

func (b *baseBlockset) GetAllBlockRefs() []agro.BlockRef {
	out := make([]agro.BlockRef, len(b.blocks))
	copy(out, b.blocks)
	return out
}

func (b *baseBlockset) String() string {
	out := "[\n"
	for _, x := range b.blocks {
		out += x.String() + "\n"
	}
	out += "]"
	return out
}
