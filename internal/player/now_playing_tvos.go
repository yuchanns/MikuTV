//go:build ios && tvos && arm64

package player

import (
	"log"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	mpRemoteCommandHandlerStatusSuccess       = 0
	mpRemoteCommandHandlerStatusCommandFailed = 200
)

var (
	mpOnce    sync.Once
	mpLoaded  bool
	mpNative  struct {
		keyTitle    objc.ID
		keyArtist   objc.ID
		keyAlbum    objc.ID
		keyDuration objc.ID
		keyElapsed  objc.ID
		keyRate     objc.ID
		keyArtwork  objc.ID

		selDefaultCenter         objc.SEL
		selSetNowPlayingInfo     objc.SEL
		selSharedCommandCenter   objc.SEL
		selPlayCommand           objc.SEL
		selPauseCommand          objc.SEL
		selTogglePlayPauseCmd    objc.SEL
		selNextTrackCommand      objc.SEL
		selPreviousTrackCommand  objc.SEL
		selChangePositionCommand objc.SEL
		selSetEnabled            objc.SEL
		selAddTargetWithHandler  objc.SEL
		selRemoveTarget          objc.SEL
		selPositionTime          objc.SEL
		selNumberWithDouble      objc.SEL
		selMutableDict           objc.SEL
		selSetObjectForKey       objc.SEL
		selDataWithBytesLength   objc.SEL
		selImageWithData         objc.SEL
		selSize                  objc.SEL
		selInitWithBoundsHandler objc.SEL

		numberWithDouble      func(objc.ID, objc.SEL, float64) objc.ID
		setObjectForKey       func(objc.ID, objc.SEL, objc.ID, objc.ID)
		dataWithBytesLength   func(objc.ID, objc.SEL, unsafe.Pointer, uintptr) objc.ID
		addTargetWithHandler  func(objc.ID, objc.SEL, objc.Block) objc.ID
		initArtworkWithBounds func(objc.ID, objc.SEL, cgSize, objc.Block) objc.ID
		getSize               func(objc.ID, objc.SEL) cgSize
		getFloat64            func(objc.ID, objc.SEL) float64
	}
)

type cgSize struct {
	W, H float64
}

func loadMediaPlayer() bool {
	mpOnce.Do(func() {
		mpLib, err := purego.Dlopen("/System/Library/Frameworks/MediaPlayer.framework/MediaPlayer", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			log.Printf("player: MediaPlayer unavailable: %v", err)
			return
		}
		libSys, err := purego.Dlopen("/usr/lib/libSystem.B.dylib", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		msgSend, err := purego.Dlsym(libSys, "objc_msgSend")
		if err != nil {
			return
		}
		for _, target := range []any{
			&mpNative.numberWithDouble,
			&mpNative.setObjectForKey,
			&mpNative.dataWithBytesLength,
			&mpNative.addTargetWithHandler,
			&mpNative.initArtworkWithBounds,
			&mpNative.getSize,
			&mpNative.getFloat64,
		} {
			purego.RegisterFunc(target, msgSend)
		}
		loadID := func(name string, dst *objc.ID) bool {
			addr, err := purego.Dlsym(mpLib, name)
			if err != nil {
				return false
			}
			*dst = objc.ID(*(*uintptr)(unsafe.Pointer(addr)))
			return *dst != 0
		}
		if !loadID("MPMediaItemPropertyTitle", &mpNative.keyTitle) ||
			!loadID("MPMediaItemPropertyArtist", &mpNative.keyArtist) ||
			!loadID("MPMediaItemPropertyAlbumTitle", &mpNative.keyAlbum) ||
			!loadID("MPMediaItemPropertyPlaybackDuration", &mpNative.keyDuration) ||
			!loadID("MPNowPlayingInfoPropertyElapsedPlaybackTime", &mpNative.keyElapsed) ||
			!loadID("MPNowPlayingInfoPropertyPlaybackRate", &mpNative.keyRate) {
			return
		}
		_ = loadID("MPMediaItemPropertyArtwork", &mpNative.keyArtwork)

		sel := objc.RegisterName
		mpNative.selDefaultCenter = sel("defaultCenter")
		mpNative.selSetNowPlayingInfo = sel("setNowPlayingInfo:")
		mpNative.selSharedCommandCenter = sel("sharedCommandCenter")
		mpNative.selPlayCommand = sel("playCommand")
		mpNative.selPauseCommand = sel("pauseCommand")
		mpNative.selTogglePlayPauseCmd = sel("togglePlayPauseCommand")
		mpNative.selNextTrackCommand = sel("nextTrackCommand")
		mpNative.selPreviousTrackCommand = sel("previousTrackCommand")
		mpNative.selChangePositionCommand = sel("changePlaybackPositionCommand")
		mpNative.selSetEnabled = sel("setEnabled:")
		mpNative.selAddTargetWithHandler = sel("addTargetWithHandler:")
		mpNative.selRemoveTarget = sel("removeTarget:")
		mpNative.selPositionTime = sel("positionTime")
		mpNative.selNumberWithDouble = sel("numberWithDouble:")
		mpNative.selMutableDict = sel("dictionary")
		mpNative.selSetObjectForKey = sel("setObject:forKey:")
		mpNative.selDataWithBytesLength = sel("dataWithBytes:length:")
		mpNative.selImageWithData = sel("imageWithData:")
		mpNative.selSize = sel("size")
		mpNative.selInitWithBoundsHandler = sel("initWithBoundsSize:requestHandler:")
		mpLoaded = true
	})
	return mpLoaded
}

type remoteCommandTarget struct {
	cmd    objc.ID
	target objc.ID
	block  objc.Block
}

func (p *tvosPlayer) clearNowPlayingArtwork() {
	n := &tvosNative
	if p.nowPlayingArtBlock != 0 {
		p.nowPlayingArtBlock.Release()
		p.nowPlayingArtBlock = 0
	}
	if p.nowPlayingArtImg != 0 {
		n.getObj(p.nowPlayingArtImg, n.selRelease)
		p.nowPlayingArtImg = 0
	}
}

func (p *tvosPlayer) setNowPlaying(info NowPlayingInfo) {
	if p.destroyed || !loadMediaPlayer() {
		return
	}
	n := &tvosNative
	mp := &mpNative
	pool := n.poolPush()
	defer n.poolPop(pool)

	center := n.getObj(objc.ID(objc.GetClass("MPNowPlayingInfoCenter")), mp.selDefaultCenter)
	if center == 0 {
		return
	}
	if !info.Active {
		n.setObj(center, mp.selSetNowPlayingInfo, 0)
		p.clearNowPlayingArtwork()
		return
	}
	dict := n.getObj(objc.ID(objc.GetClass("NSMutableDictionary")), mp.selMutableDict)
	if dict == 0 {
		return
	}
	if info.Title != "" {
		mp.setObjectForKey(dict, mp.selSetObjectForKey, nsString(info.Title), mp.keyTitle)
	}
	if info.Artist != "" {
		mp.setObjectForKey(dict, mp.selSetObjectForKey, nsString(info.Artist), mp.keyArtist)
	}
	if info.Album != "" {
		mp.setObjectForKey(dict, mp.selSetObjectForKey, nsString(info.Album), mp.keyAlbum)
	}
	nsNum := objc.ID(objc.GetClass("NSNumber"))
	if info.Duration > 0 {
		mp.setObjectForKey(dict, mp.selSetObjectForKey, mp.numberWithDouble(nsNum, mp.selNumberWithDouble, info.Duration), mp.keyDuration)
	}
	if info.Elapsed >= 0 {
		mp.setObjectForKey(dict, mp.selSetObjectForKey, mp.numberWithDouble(nsNum, mp.selNumberWithDouble, info.Elapsed), mp.keyElapsed)
	}
	mp.setObjectForKey(dict, mp.selSetObjectForKey, mp.numberWithDouble(nsNum, mp.selNumberWithDouble, info.Rate), mp.keyRate)

	var newImg objc.ID
	var newBlock objc.Block
	if len(info.Artwork) > 0 && mp.keyArtwork != 0 {
		data := mp.dataWithBytesLength(objc.ID(objc.GetClass("NSData")), mp.selDataWithBytesLength, unsafe.Pointer(&info.Artwork[0]), uintptr(len(info.Artwork)))
		if data != 0 {
			img := n.withObj(objc.ID(objc.GetClass("UIImage")), mp.selImageWithData, data)
			if img != 0 {
				n.getObj(img, n.selRetain)
				sz := mp.getSize(img, mp.selSize)
				artBlock := objc.NewBlock(func(_ objc.Block, _ cgSize) objc.ID {
					return img
				})
				alloc := n.getObj(objc.ID(objc.GetClass("MPMediaItemArtwork")), n.selAlloc)
				art := mp.initArtworkWithBounds(alloc, mp.selInitWithBoundsHandler, sz, artBlock)
				if art != 0 {
					mp.setObjectForKey(dict, mp.selSetObjectForKey, art, mp.keyArtwork)
					n.getObj(art, n.selRelease)
					newImg = img
					newBlock = artBlock
				} else {
					artBlock.Release()
					n.getObj(img, n.selRelease)
				}
			}
		}
	}
	n.setObj(center, mp.selSetNowPlayingInfo, dict)
	// Release previous artwork only after the center holds the new dictionary.
	if newImg != 0 || len(info.Artwork) == 0 {
		p.clearNowPlayingArtwork()
		p.nowPlayingArtImg = newImg
		p.nowPlayingArtBlock = newBlock
	}
}

func (p *tvosPlayer) clearRemoteCommands() {
	n := &tvosNative
	mp := &mpNative
	for _, entry := range p.remoteTargets {
		if entry.cmd != 0 && entry.target != 0 {
			n.setObj(entry.cmd, mp.selRemoveTarget, entry.target)
			n.setBool(entry.cmd, mp.selSetEnabled, false)
		}
		if entry.block != 0 {
			entry.block.Release()
		}
	}
	p.remoteTargets = nil
}

func (p *tvosPlayer) setRemoteHandlers(handlers RemoteHandlers) {
	if p.destroyed || !loadMediaPlayer() {
		return
	}
	n := &tvosNative
	mp := &mpNative
	pool := n.poolPush()
	defer n.poolPop(pool)

	p.clearRemoteCommands()
	center := n.getObj(objc.ID(objc.GetClass("MPRemoteCommandCenter")), mp.selSharedCommandCenter)
	if center == 0 {
		return
	}
	bindSimple := func(cmdSel objc.SEL, fn func()) {
		cmd := n.getObj(center, cmdSel)
		if cmd == 0 {
			return
		}
		if fn == nil {
			n.setBool(cmd, mp.selSetEnabled, false)
			return
		}
		n.setBool(cmd, mp.selSetEnabled, true)
		blk := objc.NewBlock(func(_ objc.Block, _ objc.ID) int64 {
			fn()
			return mpRemoteCommandHandlerStatusSuccess
		})
		target := mp.addTargetWithHandler(cmd, mp.selAddTargetWithHandler, blk)
		p.remoteTargets = append(p.remoteTargets, remoteCommandTarget{cmd: cmd, target: target, block: blk})
	}
	bindSimple(mp.selPlayCommand, handlers.Play)
	bindSimple(mp.selPauseCommand, handlers.Pause)
	bindSimple(mp.selTogglePlayPauseCmd, handlers.Toggle)
	bindSimple(mp.selNextTrackCommand, handlers.Next)
	bindSimple(mp.selPreviousTrackCommand, handlers.Previous)

	seekCmd := n.getObj(center, mp.selChangePositionCommand)
	if seekCmd != 0 {
		if handlers.Seek == nil {
			n.setBool(seekCmd, mp.selSetEnabled, false)
		} else {
			n.setBool(seekCmd, mp.selSetEnabled, true)
			blk := objc.NewBlock(func(_ objc.Block, ev objc.ID) int64 {
				if ev == 0 {
					return mpRemoteCommandHandlerStatusCommandFailed
				}
				pos := mp.getFloat64(ev, mp.selPositionTime)
				handlers.Seek(pos)
				return mpRemoteCommandHandlerStatusSuccess
			})
			target := mp.addTargetWithHandler(seekCmd, mp.selAddTargetWithHandler, blk)
			p.remoteTargets = append(p.remoteTargets, remoteCommandTarget{cmd: seekCmd, target: target, block: blk})
		}
	}
}
