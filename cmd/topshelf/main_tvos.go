//go:build ios && tvos && arm64

package main

import (
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/yuchanns/MikuTV/internal/topshelf"
)

const (
	sectionedImageShapeSquare uintptr = 0
	sectionedImageShapePoster uintptr = 1
	sectionedImageShapeHDTV   uintptr = 2

	imageTrait1x uintptr = 1 << 0
	imageTrait2x uintptr = 2 << 0
)

type objcBlock struct {
	isa        uintptr
	flags      int32
	reserved   int32
	invoke     uintptr
	descriptor uintptr
}

var (
	nsExtensionMain func(int32, **byte) int32
	sendSetDouble   func(objc.ID, objc.SEL, float64)
	sharedStore     *topshelf.SharedStore
	storeInitErr    error
)

func main() {
	debug.SetGCPercent(25)
	debug.SetMemoryLimit(16 << 20)

	foundation, err := purego.Dlopen("/System/Library/Frameworks/Foundation.framework/Foundation", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		log.Fatalf("topshelf: load Foundation: %v", err)
	}
	if _, err := purego.Dlopen("/System/Library/Frameworks/TVServices.framework/TVServices", purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		log.Fatalf("topshelf: load TVServices: %v", err)
	}
	libobjc, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		log.Fatalf("topshelf: load libobjc: %v", err)
	}
	purego.RegisterLibFunc(&nsExtensionMain, foundation, "NSExtensionMain")
	purego.RegisterLibFunc(&sendSetDouble, libobjc, "objc_msgSend")

	sharedStore, storeInitErr = topshelf.NewSharedStore()
	if storeInitErr != nil {
		log.Printf("topshelf: shared store init failed: %v", storeInitErr)
	} else {
		log.Printf("topshelf: initialized store mode=%s", sharedStore.Mode())
	}

	super := objc.GetClass("TVTopShelfContentProvider")
	if super == 0 {
		log.Fatal("topshelf: TVTopShelfContentProvider class unavailable")
	}
	if _, err := objc.RegisterClass(
		"MikuTVTopShelfProvider",
		super,
		nil,
		nil,
		[]objc.MethodDef{
			{
				Cmd: objc.RegisterName("loadTopShelfContentWithCompletionHandler:"),
				Fn:  loadTopShelfContent,
			},
		},
	); err != nil {
		log.Fatalf("topshelf: register provider class: %v", err)
	}

	argv := make([]*byte, 0, len(os.Args)+1)
	var keep [][]byte
	for _, arg := range os.Args {
		buf := append([]byte(arg), 0)
		keep = append(keep, buf)
		argv = append(argv, &buf[0])
	}
	argv = append(argv, nil)
	code := nsExtensionMain(int32(len(os.Args)), &argv[0])
	runtime.KeepAlive(keep)
	os.Exit(int(code))
}

func loadTopShelfContent(_ objc.ID, _ objc.SEL, completion objc.ID) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))

	content := buildSectionedContent()
	invokeCompletion(completion, content)
	if content != 0 {
		content.Send(objc.RegisterName("release"))
	}
}

func invokeCompletion(completion, content objc.ID) {
	if completion == 0 {
		return
	}
	blk := (*objcBlock)(unsafe.Pointer(completion))
	if blk.invoke == 0 {
		return
	}
	purego.SyscallN(blk.invoke, uintptr(completion), uintptr(content))
}

func buildSectionedContent() objc.ID {
	if sharedStore == nil {
		return 0
	}
	snapshot, err := sharedStore.Load()
	if err != nil {
		log.Printf("topshelf: load snapshot failed: %v", err)
		return 0
	}
	if len(snapshot.Sections) == 0 {
		log.Printf("topshelf: snapshot empty entry=%s", snapshot.EntryID)
		return 0
	}

	itemCls := objc.ID(objc.GetClass("TVTopShelfSectionedItem"))
	actionCls := objc.ID(objc.GetClass("TVTopShelfAction"))
	collectionCls := objc.ID(objc.GetClass("TVTopShelfItemCollection"))
	contentCls := objc.ID(objc.GetClass("TVTopShelfSectionedContent"))
	arrayCls := objc.ID(objc.GetClass("NSMutableArray"))
	if itemCls == 0 || actionCls == 0 || collectionCls == 0 || contentCls == 0 || arrayCls == 0 {
		return 0
	}

	selAlloc := objc.RegisterName("alloc")
	selRelease := objc.RegisterName("release")
	selArrayCap := objc.RegisterName("arrayWithCapacity:")
	selAddObj := objc.RegisterName("addObject:")
	selCount := objc.RegisterName("count")
	selInitID := objc.RegisterName("initWithIdentifier:")
	selSetTitle := objc.RegisterName("setTitle:")
	selSetImageShape := objc.RegisterName("setImageShape:")
	selSetImageURL := objc.RegisterName("setImageURL:forTraits:")
	selSetProgress := objc.RegisterName("setPlaybackProgress:")
	selInitURL := objc.RegisterName("initWithURL:")
	selSetDisplay := objc.RegisterName("setDisplayAction:")
	selSetPlay := objc.RegisterName("setPlayAction:")
	selInitItems := objc.RegisterName("initWithItems:")
	selInitSections := objc.RegisterName("initWithSections:")

	sectionArr := arrayCls.Send(selArrayCap, uintptr(len(snapshot.Sections)))
	totalItems := 0
	for _, sec := range snapshot.Sections {
		if len(sec.Items) == 0 {
			continue
		}
		itemArr := arrayCls.Send(selArrayCap, uintptr(len(sec.Items)))
		for _, entry := range sec.Items {
			ident := nsString(entry.EntryID + ":" + entry.Kind + ":" + entry.ID)
			item := itemCls.Send(selAlloc).Send(selInitID, ident)
			if item == 0 {
				continue
			}
			item.Send(selSetTitle, nsString(entry.Title))
			item.Send(selSetImageShape, sectionedImageShapeHDTV)
			if imgURL := nsURL(entry.ImageURL); imgURL != 0 {
				item.Send(selSetImageURL, imgURL, imageTrait1x|imageTrait2x)
			}
			if progress := entry.PlaybackProgress(); progress > 0 {
				sendSetDouble(item, selSetProgress, progress)
			}
			if detailRaw := topshelf.BuildURL(topshelf.Link{
				Action:  topshelf.ActionDetail,
				EntryID: entry.EntryID,
				Kind:    entry.Kind,
				ID:      entry.ID,
			}); detailRaw != "" {
				if u := nsURL(detailRaw); u != 0 {
					action := actionCls.Send(selAlloc).Send(selInitURL, u)
					if action != 0 {
						item.Send(selSetDisplay, action)
						action.Send(selRelease)
					}
				}
			}
			if playRaw := topshelf.BuildURL(topshelf.Link{
				Action:  topshelf.ActionPlay,
				EntryID: entry.EntryID,
				Kind:    entry.Kind,
				ID:      entry.ID,
			}); playRaw != "" {
				if u := nsURL(playRaw); u != 0 {
					action := actionCls.Send(selAlloc).Send(selInitURL, u)
					if action != 0 {
						item.Send(selSetPlay, action)
						action.Send(selRelease)
					}
				}
			}
			itemArr.Send(selAddObj, item)
			item.Send(selRelease)
			totalItems++
		}
		if itemArr.Send(selCount) == 0 {
			continue
		}
		col := collectionCls.Send(selAlloc).Send(selInitItems, itemArr)
		if col == 0 {
			continue
		}
		if sec.Title != "" {
			col.Send(selSetTitle, nsString(sec.Title))
		}
		sectionArr.Send(selAddObj, col)
		col.Send(selRelease)
	}
	if sectionArr.Send(selCount) == 0 {
		return 0
	}
	content := contentCls.Send(selAlloc).Send(selInitSections, sectionArr)
	log.Printf("topshelf: served sections=%d items=%d entry=%s", int(sectionArr.Send(selCount)), totalItems, snapshot.EntryID)
	return content
}

func nsString(value string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), value)
}

func nsURL(value string) objc.ID {
	if value == "" {
		return 0
	}
	return objc.ID(objc.GetClass("NSURL")).Send(objc.RegisterName("URLWithString:"), nsString(value))
}
