//go:build ios && tvos && arm64

package player

import (
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"structs"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type cgRect struct {
	_          structs.HostLayout
	X, Y, W, H float64
}

type cgAffineTransform struct {
	_                  structs.HostLayout
	A, B, C, D, Tx, Ty float64
}

type cmTime struct {
	_         structs.HostLayout
	Value     int64
	Timescale int32
	Flags     uint32
	Epoch     int64
}

type cmTimeRange struct {
	_        structs.HostLayout
	Start    cmTime
	Duration cmTime
}

const (
	cmTimeFlagValid            = 1 << 0
	cmTimeFlagPositiveInfinity = 1 << 2
	cmTimeFlagNegativeInfinity = 1 << 3
	cmTimeFlagIndefinite       = 1 << 4
	cmTimeNumericMask          = cmTimeFlagValid | cmTimeFlagPositiveInfinity | cmTimeFlagNegativeInfinity | cmTimeFlagIndefinite

	cvPixelFormat32BGRA            = 0x42475241 // 'BGRA'
	cmVideoCodecHEVC               = 0x68766331 // 'hvc1'
	cgImageAlphaPremultipliedLast  = 1
	uiViewAutoresizingFlexibleSize = (1 << 1) | (1 << 4)
	uiViewContentModeAspectFit     = 1
	uiApplicationStateBackground   = 2
	avKeyValueStatusLoaded         = 2
	avPlayerItemStatusUnknown      = 0
	avPlayerItemStatusReadyToPlay  = 1
	avPlayerItemStatusFailed       = 2
	avPlayerTimeControlWaiting     = 1
	avAudioSessionDeactivateNotify = 1
	avAudioSessionInterruptEnded   = 0
	avAudioSessionInterruptBegan   = 1
	avAudioSessionOptShouldResume  = 1
	avErrorNoCompatibleAlternates  = -11868
	nsKeyValueObservingOptionNew   = 1

	kvoContextToken uintptr = 0x4d696b7556696465 // "MikuVide"
)

func cmTimeIsNumeric(t cmTime) bool {
	return t.Flags&cmTimeNumericMask == cmTimeFlagValid
}

var (
	tvosLoadOnce sync.Once
	tvosLoadErr  error

	tvosNative struct {
		// Runtime & threading.
		poolPush       func() uintptr
		poolPop        func(uintptr)
		mainThread     func() int32
		objectGetClass func(objc.ID) objc.Class

		// Typed objc_msgSend entry points for ABI accuracy and zero reflection overhead.
		getRect            func(objc.ID, objc.SEL) cgRect
		setRect            func(objc.ID, objc.SEL, cgRect)
		initRect           func(objc.ID, objc.SEL, cgRect) objc.ID
		setFloat32         func(objc.ID, objc.SEL, float32)
		getFloat32         func(objc.ID, objc.SEL) float32
		getFloat64         func(objc.ID, objc.SEL) float64
		setFloat64         func(objc.ID, objc.SEL, float64)
		blurSigma          func(objc.ID, objc.SEL, float64) objc.ID
		applyTransform     func(objc.ID, objc.SEL, cgAffineTransform) objc.ID
		createCGImage      func(objc.ID, objc.SEL, objc.ID, cgRect) uintptr
		getCMTime          func(objc.ID, objc.SEL) cmTime
		getCMTimeRange     func(objc.ID, objc.SEL) cmTimeRange
		itemTimeForHost    func(objc.ID, objc.SEL, float64) cmTime
		hasNewPixelBuffer  func(objc.ID, objc.SEL, cmTime) bool
		copyPixelBuffer    func(objc.ID, objc.SEL, cmTime, uintptr) uintptr
		seekToTime         func(objc.ID, objc.SEL, cmTime, cmTime, cmTime, objc.Block)
		initDisplayCrit    func(objc.ID, objc.SEL, float32, uintptr) objc.ID
		statusForKey       func(objc.ID, objc.SEL, objc.ID, *objc.ID) int64
		setAudioCategory   func(objc.ID, objc.SEL, objc.ID, objc.ID, uintptr, uintptr) bool
		setAudioActive     func(objc.ID, objc.SEL, bool, uintptr) bool
		setAudioActiveOpts func(objc.ID, objc.SEL, bool, uintptr, uintptr) bool
		getBool            func(objc.ID, objc.SEL) bool
		getBoolObj         func(objc.ID, objc.SEL, objc.ID) bool
		setBool            func(objc.ID, objc.SEL, bool)
		getInt64           func(objc.ID, objc.SEL) int64
		getUintptr         func(objc.ID, objc.SEL) uintptr
		setUintptr         func(objc.ID, objc.SEL, uintptr)
		insertSubview      func(objc.ID, objc.SEL, objc.ID, int64)
		addKVO             func(objc.ID, objc.SEL, objc.ID, objc.ID, uintptr, uintptr)
		removeKVO          func(objc.ID, objc.SEL, objc.ID, objc.ID, uintptr)
		loadValues         func(objc.ID, objc.SEL, objc.ID, objc.Block)
		addOpBlock         func(objc.ID, objc.SEL, objc.Block)
		addObserverSel     func(objc.ID, objc.SEL, objc.ID, objc.SEL, objc.ID, objc.ID)
		setObj             func(objc.ID, objc.SEL, objc.ID)
		setObj2            func(objc.ID, objc.SEL, objc.ID, objc.ID)
		getObj             func(objc.ID, objc.SEL) objc.ID
		getObjIdx          func(objc.ID, objc.SEL, uintptr) objc.ID
		getObjKey          func(objc.ID, objc.SEL, objc.ID) objc.ID
		withObj            func(objc.ID, objc.SEL, objc.ID) objc.ID
		withObj2           func(objc.ID, objc.SEL, objc.ID, objc.ID) objc.ID
		withUint32         func(objc.ID, objc.SEL, uint32) objc.ID
		withPtr            func(objc.ID, objc.SEL, uintptr) objc.ID
		arrayWithObjects   func(objc.ID, objc.SEL, unsafe.Pointer, uintptr) objc.ID

		// CoreMedia / CoreVideo / CoreGraphics / QuartzCore / VideoToolbox / CoreFoundation.
		cfEqual               func(uintptr, uintptr) bool
		caCurrentMediaTime    func() float64
		cmTimeMakeWithSeconds func(float64, int32) cmTime
		cmTimeGetSeconds      func(cmTime) float64
		cmTimeRangeGetEnd     func(cmTimeRange) cmTime
		cmTimeZero            cmTime
		cmFmtGetExtension     func(uintptr, uintptr) uintptr
		cmFmtGetMediaSubType  func(uintptr) uint32
		cvPixelBufferRelease  func(uintptr)
		cgColorSpaceCreateRGB func() uintptr
		cgColorSpaceRelease   func(uintptr)
		cgBitmapContextCreate func(unsafe.Pointer, uintptr, uintptr, uintptr, uintptr, uintptr, uint32) uintptr
		cgContextDrawImage    func(uintptr, cgRect, uintptr)
		cgContextRelease      func(uintptr)
		cgImageRelease        func(uintptr)
		vtHardwareDecode      func(uint32) bool

		// Framework string constants.
		kCVPixelBufferPixelFormatTypeKey      objc.ID
		kCMFmtExtTransferFunction             uintptr
		kCMFmtExtAltTransferCharacteristics   uintptr
		kCMFmtExtColorPrimaries               uintptr
		kCMFmtTransferPQ                      uintptr
		kCMFmtTransferHLG                     uintptr
		uiAppWillResignActiveNotification     objc.ID
		uiAppDidBecomeActiveNotification      objc.ID
		avPlayerItemDidPlayToEndNotification      objc.ID
		avPlayerItemFailedToPlayNotification      objc.ID
		avPlayerItemFailedErrorKey                objc.ID
		avPlayerItemPlaybackStalledNotification   objc.ID
		avPlayerItemNewAccessLogEntryNotification objc.ID
		avDisplayModeSwitchStartNotification    objc.ID
		avDisplayModeSwitchEndNotification      objc.ID
		avDisplayModeSwitchSettingsNotification objc.ID
		avLayerVideoGravityResizeAspect         objc.ID
		avLayerVideoGravityResizeAspectFill     objc.ID
		avAudioSessionCategoryPlayback          objc.ID
		avAudioSessionModeMoviePlayback         objc.ID
		avAudioSessionInterruptionNotification  objc.ID
		avAudioSessionInterruptionTypeKey       objc.ID
		avAudioSessionInterruptionOptionKey     objc.ID
		avMediaTypeAudio                      objc.ID
		avMediaTypeVideo                      objc.ID
		avMediaCharacteristicAudible          objc.ID
		avMediaCharacteristicContainsHDRVideo objc.ID

		// Cached selectors.
		selAlloc, selInit, selRetain, selRelease                    objc.SEL
		selStringWithUTF8, selUTF8String, selDescription            objc.SEL
		selURLWithString, selURLAssetWithURL                        objc.SEL
		selPlayerWithItem, selPlayerItemWithAsset                   objc.SEL
		selReplaceCurrentItem, selCurrentItem                       objc.SEL
		selSetAllowsExtPlayback, selSetPreventsSleep                objc.SEL
		selSetPrefForwardBuffer                                     objc.SEL
		selRootViewController, selView, selLayer                    objc.SEL
		selBounds, selFrame, selSetFrame, selInitWithFrame          objc.SEL
		selSetOpaque, selSetAutoresizingMask, selSetUserInteract    objc.SEL
		selSetClipsToBounds                                         objc.SEL
		selSetHidden, selIsHidden, selSetBackgroundColor            objc.SEL
		selBlackColor, selSetZPosition, selSetVideoGravity          objc.SEL
		selSetPlayer, selInsertSubviewAtIndex, selAddSubview        objc.SEL
		selRemoveFromSuperview, selSuperview                        objc.SEL
		selSetContentMode, selSetImage                              objc.SEL
		selSharedApplication, selApplicationState                   objc.SEL
		selIsIdleTimerDisabled, selSetIdleTimerDisabled             objc.SEL
		selDefaultCenter, selAddObserver, selRemoveObserver         objc.SEL
		selObject, selUserInfo, selObjectForKey                     objc.SEL
		selMainQueue, selAddOperationWithBlock                      objc.SEL
		selAVDisplayManager, selPrefDisplayCriteria                 objc.SEL
		selSetPrefDisplayCriteria, selDisplayCriteriaMatching       objc.SEL
		selDisplayModeSwitchInProgress, selInitRefreshFormat        objc.SEL
		selIsEqual, selIsEqualToString                              objc.SEL
		selLoadValuesAsynchronously, selStatusOfValueForKey         objc.SEL
		selCancelLoading, selIsPlayable                             objc.SEL
		selInitWithPixelBufferAttrs, selAddOutput, selRemoveOutput objc.SEL
		selPause, selSetRate, selSeekToTime, selCancelPendingSeeks objc.SEL
		selCurrentTime, selDuration, selLoadedTimeRanges           objc.SEL
		selCMTimeRangeValue, selStatus, selError, selCode          objc.SEL
		selTimeControlStatus, selReasonForWaitingToPlay            objc.SEL
		selIsReadyForDisplay                                       objc.SEL
		selAccessLog, selEvents, selLastObject                     objc.SEL
		selNumberOfStalls, selNumberOfDroppedVideoFrames           objc.SEL
		selIndicatedBitrate, selObservedBitrate, selDurationWatched objc.SEL
		selTracks, selIsEnabled, selAssetTrack, selMediaType        objc.SEL
		selMediaSelectionGroup, selOptions, selDisplayName          objc.SEL
		selSelectMediaOption                                        objc.SEL
		selFormatDescriptions, selFirstObject, selNominalFrameRate  objc.SEL
		selVariants, selVideoAttributes                             objc.SEL
		selHasMediaCharacteristic, selCount, selObjectAtIndex       objc.SEL
		selArrayWithObjectsCount, selDictWithObjectForKey           objc.SEL
		selNumberWithUnsignedInt, selUnsignedIntegerValue           objc.SEL
		selItemTimeForHostTime, selHasNewPixelBuffer                objc.SEL
		selCopyPixelBuffer, selImageWithCVPixelBuffer               objc.SEL
		selExtent, selImageByApplyingTransform                      objc.SEL
		selImageByClampingToExtent, selImageByApplyingGaussianBlur  objc.SEL
		selContextWithOptions, selCreateCGImageFromRect             objc.SEL
		selImageWithCGImage                                         objc.SEL
		selSharedInstance, selSetCategoryMode, selSetActive         objc.SEL
		selSetActiveWithOptions, selEligibleForHDRPlayback          objc.SEL
		selAddKVO, selRemoveKVO                                     objc.SEL
		selHandleResignActive, selHandleBecomeActive                objc.SEL
		selHandleItemEnded, selHandleItemFailed                     objc.SEL
		selHandleItemStalled, selHandleAccessLog                    objc.SEL
		selHandleModeSwitchStart, selHandleModeSwitchEnd            objc.SEL
		selHandleModeSwitchSettings, selHandleAudioInterruption     objc.SEL

		// Retained constant NSStrings.
		strPlayable               objc.ID
		strPreferredDisplayCrit   objc.ID
		strAvailMediaChars        objc.ID
		strStatus                 objc.ID
		strTracks                 objc.ID
		strReadyForDisplay        objc.ID
		assetLoadKeys             objc.ID
		layerClass                objc.Class
		observerClass             objc.Class
	}

	observerMu sync.Mutex
	observers  = make(map[objc.ID]*tvosPlayer)
)

func loadTVOSPlayer() error {
	tvosLoadOnce.Do(func() {
		libs := make(map[string]uintptr)
		for _, path := range []string{
			"/usr/lib/libSystem.B.dylib",
			"/usr/lib/libobjc.A.dylib",
			"/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			"/System/Library/Frameworks/CoreImage.framework/CoreImage",
			"/System/Library/Frameworks/CoreMedia.framework/CoreMedia",
			"/System/Library/Frameworks/CoreVideo.framework/CoreVideo",
			"/System/Library/Frameworks/QuartzCore.framework/QuartzCore",
			"/System/Library/Frameworks/UIKit.framework/UIKit",
			"/System/Library/Frameworks/AVFoundation.framework/AVFoundation",
			"/System/Library/Frameworks/AVFAudio.framework/AVFAudio",
			"/System/Library/Frameworks/AVKit.framework/AVKit",
			"/System/Library/Frameworks/VideoToolbox.framework/VideoToolbox",
		} {
			h, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				tvosLoadErr = fmt.Errorf("player: dlopen %s: %w", path, err)
				return
			}
			libs[path] = h
		}

		sysLib := libs["/usr/lib/libSystem.B.dylib"]
		objcLib := libs["/usr/lib/libobjc.A.dylib"]
		cfLib := libs["/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"]
		cgLib := libs["/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"]
		cmLib := libs["/System/Library/Frameworks/CoreMedia.framework/CoreMedia"]
		cvLib := libs["/System/Library/Frameworks/CoreVideo.framework/CoreVideo"]
		qcLib := libs["/System/Library/Frameworks/QuartzCore.framework/QuartzCore"]
		uiLib := libs["/System/Library/Frameworks/UIKit.framework/UIKit"]
		avLib := libs["/System/Library/Frameworks/AVFoundation.framework/AVFoundation"]
		avAudioLib := libs["/System/Library/Frameworks/AVFAudio.framework/AVFAudio"]
		avKitLib := libs["/System/Library/Frameworks/AVKit.framework/AVKit"]
		vtLib := libs["/System/Library/Frameworks/VideoToolbox.framework/VideoToolbox"]

		msgSend, err := purego.Dlsym(objcLib, "objc_msgSend")
		if err != nil {
			tvosLoadErr = err
			return
		}
		n := &tvosNative
		purego.RegisterLibFunc(&n.poolPush, objcLib, "objc_autoreleasePoolPush")
		purego.RegisterLibFunc(&n.poolPop, objcLib, "objc_autoreleasePoolPop")
		purego.RegisterLibFunc(&n.objectGetClass, objcLib, "object_getClass")
		purego.RegisterLibFunc(&n.mainThread, sysLib, "pthread_main_np")

		for _, target := range []any{
			&n.getRect, &n.setRect, &n.initRect, &n.setFloat32, &n.getFloat32, &n.getFloat64, &n.setFloat64,
			&n.blurSigma, &n.applyTransform, &n.createCGImage,
			&n.getCMTime, &n.getCMTimeRange, &n.itemTimeForHost, &n.hasNewPixelBuffer,
			&n.copyPixelBuffer, &n.seekToTime, &n.initDisplayCrit, &n.statusForKey,
			&n.setAudioCategory, &n.setAudioActive, &n.setAudioActiveOpts,
			&n.getBool, &n.getBoolObj, &n.setBool, &n.getInt64, &n.getUintptr, &n.setUintptr,
			&n.insertSubview, &n.addKVO, &n.removeKVO, &n.loadValues, &n.addOpBlock,
			&n.addObserverSel, &n.setObj, &n.setObj2, &n.getObj, &n.getObjIdx, &n.getObjKey,
			&n.withObj, &n.withObj2, &n.withUint32, &n.withPtr, &n.arrayWithObjects,
		} {
			purego.RegisterFunc(target, msgSend)
		}

		for _, sym := range []struct {
			lib    uintptr
			name   string
			target any
		}{
			{cfLib, "CFEqual", &n.cfEqual},
			{qcLib, "CACurrentMediaTime", &n.caCurrentMediaTime},
			{cmLib, "CMTimeMakeWithSeconds", &n.cmTimeMakeWithSeconds},
			{cmLib, "CMTimeGetSeconds", &n.cmTimeGetSeconds},
			{cmLib, "CMTimeRangeGetEnd", &n.cmTimeRangeGetEnd},
			{cmLib, "CMFormatDescriptionGetExtension", &n.cmFmtGetExtension},
			{cmLib, "CMFormatDescriptionGetMediaSubType", &n.cmFmtGetMediaSubType},
			{cvLib, "CVPixelBufferRelease", &n.cvPixelBufferRelease},
			{cgLib, "CGColorSpaceCreateDeviceRGB", &n.cgColorSpaceCreateRGB},
			{cgLib, "CGColorSpaceRelease", &n.cgColorSpaceRelease},
			{cgLib, "CGBitmapContextCreate", &n.cgBitmapContextCreate},
			{cgLib, "CGContextDrawImage", &n.cgContextDrawImage},
			{cgLib, "CGContextRelease", &n.cgContextRelease},
			{cgLib, "CGImageRelease", &n.cgImageRelease},
			{vtLib, "VTIsHardwareDecodeSupported", &n.vtHardwareDecode},
		} {
			addr, err := purego.Dlsym(sym.lib, sym.name)
			if err != nil {
				tvosLoadErr = fmt.Errorf("player: dlsym %s: %w", sym.name, err)
				return
			}
			purego.RegisterFunc(sym.target, addr)
		}

		if zeroAddr, err := purego.Dlsym(cmLib, "kCMTimeZero"); err == nil {
			n.cmTimeZero = *(*cmTime)(unsafe.Pointer(zeroAddr))
		} else {
			n.cmTimeZero = cmTime{Timescale: 1, Flags: cmTimeFlagValid}
		}

		loadPtr := func(lib uintptr, name string) (uintptr, error) {
			addr, err := purego.Dlsym(lib, name)
			if err != nil {
				return 0, fmt.Errorf("player: dlsym %s: %w", name, err)
			}
			return *(*uintptr)(unsafe.Pointer(addr)), nil
		}
		for _, c := range []struct {
			lib    uintptr
			name   string
			target *uintptr
		}{
			{cmLib, "kCMFormatDescriptionExtension_TransferFunction", &n.kCMFmtExtTransferFunction},
			{cmLib, "kCMFormatDescriptionExtension_AlternativeTransferCharacteristics", &n.kCMFmtExtAltTransferCharacteristics},
			{cmLib, "kCMFormatDescriptionExtension_ColorPrimaries", &n.kCMFmtExtColorPrimaries},
			{cmLib, "kCMFormatDescriptionTransferFunction_SMPTE_ST_2084_PQ", &n.kCMFmtTransferPQ},
			{cmLib, "kCMFormatDescriptionTransferFunction_ITU_R_2100_HLG", &n.kCMFmtTransferHLG},
		} {
			val, err := loadPtr(c.lib, c.name)
			if err != nil {
				tvosLoadErr = err
				return
			}
			*c.target = val
		}
		for _, c := range []struct {
			lib    uintptr
			name   string
			target *objc.ID
		}{
			{cvLib, "kCVPixelBufferPixelFormatTypeKey", &n.kCVPixelBufferPixelFormatTypeKey},
			{uiLib, "UIApplicationWillResignActiveNotification", &n.uiAppWillResignActiveNotification},
			{uiLib, "UIApplicationDidBecomeActiveNotification", &n.uiAppDidBecomeActiveNotification},
			{avLib, "AVPlayerItemDidPlayToEndTimeNotification", &n.avPlayerItemDidPlayToEndNotification},
			{avLib, "AVPlayerItemFailedToPlayToEndTimeNotification", &n.avPlayerItemFailedToPlayNotification},
			{avLib, "AVPlayerItemFailedToPlayToEndTimeErrorKey", &n.avPlayerItemFailedErrorKey},
			{avLib, "AVPlayerItemPlaybackStalledNotification", &n.avPlayerItemPlaybackStalledNotification},
			{avLib, "AVPlayerItemNewAccessLogEntryNotification", &n.avPlayerItemNewAccessLogEntryNotification},
			{avKitLib, "AVDisplayManagerModeSwitchStartNotification", &n.avDisplayModeSwitchStartNotification},
			{avKitLib, "AVDisplayManagerModeSwitchEndNotification", &n.avDisplayModeSwitchEndNotification},
			{avKitLib, "AVDisplayManagerModeSwitchSettingsChangedNotification", &n.avDisplayModeSwitchSettingsNotification},
			{avLib, "AVLayerVideoGravityResizeAspect", &n.avLayerVideoGravityResizeAspect},
			{avLib, "AVLayerVideoGravityResizeAspectFill", &n.avLayerVideoGravityResizeAspectFill},
			{avAudioLib, "AVAudioSessionCategoryPlayback", &n.avAudioSessionCategoryPlayback},
			{avAudioLib, "AVAudioSessionModeMoviePlayback", &n.avAudioSessionModeMoviePlayback},
			{avAudioLib, "AVAudioSessionInterruptionNotification", &n.avAudioSessionInterruptionNotification},
			{avAudioLib, "AVAudioSessionInterruptionTypeKey", &n.avAudioSessionInterruptionTypeKey},
			{avAudioLib, "AVAudioSessionInterruptionOptionKey", &n.avAudioSessionInterruptionOptionKey},
			{avLib, "AVMediaTypeAudio", &n.avMediaTypeAudio},
			{avLib, "AVMediaTypeVideo", &n.avMediaTypeVideo},
			{avLib, "AVMediaCharacteristicAudible", &n.avMediaCharacteristicAudible},
			{avLib, "AVMediaCharacteristicContainsHDRVideo", &n.avMediaCharacteristicContainsHDRVideo},
		} {
			val, err := loadPtr(c.lib, c.name)
			if err != nil {
				tvosLoadErr = err
				return
			}
			*c.target = objc.ID(val)
		}

		// Register selectors once.
		sel := objc.RegisterName
		n.selAlloc = sel("alloc")
		n.selInit = sel("init")
		n.selRetain = sel("retain")
		n.selRelease = sel("release")
		n.selStringWithUTF8 = sel("stringWithUTF8String:")
		n.selUTF8String = sel("UTF8String")
		n.selDescription = sel("description")
		n.selURLWithString = sel("URLWithString:")
		n.selURLAssetWithURL = sel("URLAssetWithURL:options:")
		n.selPlayerWithItem = sel("playerWithPlayerItem:")
		n.selPlayerItemWithAsset = sel("playerItemWithAsset:")
		n.selReplaceCurrentItem = sel("replaceCurrentItemWithPlayerItem:")
		n.selCurrentItem = sel("currentItem")
		n.selSetAllowsExtPlayback = sel("setAllowsExternalPlayback:")
		n.selSetPreventsSleep = sel("setPreventsDisplaySleepDuringVideoPlayback:")
		n.selSetPrefForwardBuffer = sel("setPreferredForwardBufferDuration:")
		n.selRootViewController = sel("rootViewController")
		n.selView = sel("view")
		n.selLayer = sel("layer")
		n.selBounds = sel("bounds")
		n.selFrame = sel("frame")
		n.selSetFrame = sel("setFrame:")
		n.selInitWithFrame = sel("initWithFrame:")
		n.selSetOpaque = sel("setOpaque:")
		n.selSetClipsToBounds = sel("setClipsToBounds:")
		n.selSetAutoresizingMask = sel("setAutoresizingMask:")
		n.selSetUserInteract = sel("setUserInteractionEnabled:")
		n.selSetHidden = sel("setHidden:")
		n.selIsHidden = sel("isHidden")
		n.selSetBackgroundColor = sel("setBackgroundColor:")
		n.selBlackColor = sel("blackColor")
		n.selSetZPosition = sel("setZPosition:")
		n.selSetVideoGravity = sel("setVideoGravity:")
		n.selSetPlayer = sel("setPlayer:")
		n.selInsertSubviewAtIndex = sel("insertSubview:atIndex:")
		n.selAddSubview = sel("addSubview:")
		n.selRemoveFromSuperview = sel("removeFromSuperview")
		n.selSuperview = sel("superview")
		n.selSetContentMode = sel("setContentMode:")
		n.selSetImage = sel("setImage:")
		n.selSharedApplication = sel("sharedApplication")
		n.selApplicationState = sel("applicationState")
		n.selIsIdleTimerDisabled = sel("isIdleTimerDisabled")
		n.selSetIdleTimerDisabled = sel("setIdleTimerDisabled:")
		n.selDefaultCenter = sel("defaultCenter")
		n.selAddObserver = sel("addObserver:selector:name:object:")
		n.selRemoveObserver = sel("removeObserver:")
		n.selObject = sel("object")
		n.selUserInfo = sel("userInfo")
		n.selObjectForKey = sel("objectForKey:")
		n.selMainQueue = sel("mainQueue")
		n.selAddOperationWithBlock = sel("addOperationWithBlock:")
		n.selAVDisplayManager = sel("avDisplayManager")
		n.selPrefDisplayCriteria = sel("preferredDisplayCriteria")
		n.selSetPrefDisplayCriteria = sel("setPreferredDisplayCriteria:")
		n.selDisplayCriteriaMatching = sel("isDisplayCriteriaMatchingEnabled")
		n.selDisplayModeSwitchInProgress = sel("isDisplayModeSwitchInProgress")
		n.selInitRefreshFormat = sel("initWithRefreshRate:formatDescription:")
		n.selIsEqual = sel("isEqual:")
		n.selIsEqualToString = sel("isEqualToString:")
		n.selLoadValuesAsynchronously = sel("loadValuesAsynchronouslyForKeys:completionHandler:")
		n.selStatusOfValueForKey = sel("statusOfValueForKey:error:")
		n.selCancelLoading = sel("cancelLoading")
		n.selIsPlayable = sel("isPlayable")
		n.selInitWithPixelBufferAttrs = sel("initWithPixelBufferAttributes:")
		n.selAddOutput = sel("addOutput:")
		n.selRemoveOutput = sel("removeOutput:")
		n.selPause = sel("pause")
		n.selSetRate = sel("setRate:")
		n.selSeekToTime = sel("seekToTime:toleranceBefore:toleranceAfter:completionHandler:")
		n.selCancelPendingSeeks = sel("cancelPendingSeeks")
		n.selCurrentTime = sel("currentTime")
		n.selDuration = sel("duration")
		n.selLoadedTimeRanges = sel("loadedTimeRanges")
		n.selCMTimeRangeValue = sel("CMTimeRangeValue")
		n.selStatus = sel("status")
		n.selError = sel("error")
		n.selCode = sel("code")
		n.selTimeControlStatus = sel("timeControlStatus")
		n.selReasonForWaitingToPlay = sel("reasonForWaitingToPlay")
		n.selIsReadyForDisplay = sel("isReadyForDisplay")
		n.selAccessLog = sel("accessLog")
		n.selEvents = sel("events")
		n.selLastObject = sel("lastObject")
		n.selNumberOfStalls = sel("numberOfStalls")
		n.selNumberOfDroppedVideoFrames = sel("numberOfDroppedVideoFrames")
		n.selIndicatedBitrate = sel("indicatedBitrate")
		n.selObservedBitrate = sel("observedBitrate")
		n.selDurationWatched = sel("durationWatched")
		n.selTracks = sel("tracks")
		n.selIsEnabled = sel("isEnabled")
		n.selAssetTrack = sel("assetTrack")
		n.selMediaType = sel("mediaType")
		n.selMediaSelectionGroup = sel("mediaSelectionGroupForMediaCharacteristic:")
		n.selOptions = sel("options")
		n.selDisplayName = sel("displayName")
		n.selSelectMediaOption = sel("selectMediaOption:inMediaSelectionGroup:")
		n.selFormatDescriptions = sel("formatDescriptions")
		n.selFirstObject = sel("firstObject")
		n.selNominalFrameRate = sel("nominalFrameRate")
		n.selVariants = sel("variants")
		n.selVideoAttributes = sel("videoAttributes")
		n.selHasMediaCharacteristic = sel("hasMediaCharacteristic:")
		n.selCount = sel("count")
		n.selObjectAtIndex = sel("objectAtIndex:")
		n.selArrayWithObjectsCount = sel("arrayWithObjects:count:")
		n.selDictWithObjectForKey = sel("dictionaryWithObject:forKey:")
		n.selNumberWithUnsignedInt = sel("numberWithUnsignedInt:")
		n.selUnsignedIntegerValue = sel("unsignedIntegerValue")
		n.selItemTimeForHostTime = sel("itemTimeForHostTime:")
		n.selHasNewPixelBuffer = sel("hasNewPixelBufferForItemTime:")
		n.selCopyPixelBuffer = sel("copyPixelBufferForItemTime:itemTimeForDisplay:")
		n.selImageWithCVPixelBuffer = sel("imageWithCVPixelBuffer:")
		n.selExtent = sel("extent")
		n.selImageByApplyingTransform = sel("imageByApplyingTransform:")
		n.selImageByClampingToExtent = sel("imageByClampingToExtent")
		n.selImageByApplyingGaussianBlur = sel("imageByApplyingGaussianBlurWithSigma:")
		n.selContextWithOptions = sel("contextWithOptions:")
		n.selCreateCGImageFromRect = sel("createCGImage:fromRect:")
		n.selImageWithCGImage = sel("imageWithCGImage:")
		n.selSharedInstance = sel("sharedInstance")
		n.selSetCategoryMode = sel("setCategory:mode:options:error:")
		n.selSetActive = sel("setActive:error:")
		n.selSetActiveWithOptions = sel("setActive:withOptions:error:")
		n.selEligibleForHDRPlayback = sel("eligibleForHDRPlayback")
		n.selAddKVO = sel("addObserver:forKeyPath:options:context:")
		n.selRemoveKVO = sel("removeObserver:forKeyPath:context:")
		n.selHandleResignActive = sel("handleResignActive:")
		n.selHandleBecomeActive = sel("handleBecomeActive:")
		n.selHandleItemEnded = sel("handleItemEnded:")
		n.selHandleItemFailed = sel("handleItemFailed:")
		n.selHandleItemStalled = sel("handleItemStalled:")
		n.selHandleAccessLog = sel("handleAccessLog:")
		n.selHandleModeSwitchStart = sel("handleModeSwitchStart:")
		n.selHandleModeSwitchEnd = sel("handleModeSwitchEnd:")
		n.selHandleModeSwitchSettings = sel("handleModeSwitchSettings:")
		n.selHandleAudioInterruption = sel("handleAudioInterruption:")

		pool := n.poolPush()
		defer n.poolPop(pool)

		n.strPlayable = retainNSString("playable")
		n.strPreferredDisplayCrit = retainNSString("preferredDisplayCriteria")
		n.strAvailMediaChars = retainNSString("availableMediaCharacteristicsWithMediaSelectionOptions")
		n.strStatus = retainNSString("status")
		n.strTracks = retainNSString("tracks")
		n.strReadyForDisplay = retainNSString("readyForDisplay")
		keys := [3]objc.ID{n.strPlayable, n.strPreferredDisplayCrit, n.strAvailMediaChars}
		arr := n.arrayWithObjects(objc.ID(objc.GetClass("NSArray")), n.selArrayWithObjectsCount, unsafe.Pointer(&keys[0]), 3)
		n.getObj(arr, n.selRetain)
		n.assetLoadKeys = arr

		layerCls, err := objc.RegisterClass("MikuVideoLayer", objc.GetClass("UIView"), nil, nil, nil)
		if err != nil {
			tvosLoadErr = fmt.Errorf("player: register MikuVideoLayer: %w", err)
			return
		}
		meta := n.objectGetClass(objc.ID(layerCls))
		avPlayerLayer := objc.GetClass("AVPlayerLayer")
		if !meta.AddMethod(sel("layerClass"), objc.NewIMP(func(objc.ID, objc.SEL) objc.Class { return avPlayerLayer }), "#@:") {
			tvosLoadErr = fmt.Errorf("player: add +layerClass on MikuVideoLayer")
			return
		}
		n.layerClass = layerCls

		obsCls, err := objc.RegisterClass("MikuVideoObserver", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
			{Cmd: n.selHandleResignActive, Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
				if p := lookupObserver(self); p != nil {
					p.onResignActive()
				}
			}},
			{Cmd: n.selHandleBecomeActive, Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
				if p := lookupObserver(self); p != nil {
					p.onBecomeActive()
				}
			}},
			{Cmd: n.selHandleItemEnded, Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
				if p := lookupObserver(self); p != nil {
					p.onItemEnded(note)
				}
			}},
			{Cmd: n.selHandleItemFailed, Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
				if p := lookupObserver(self); p != nil {
					p.onItemFailed(note)
				}
			}},
			{Cmd: n.selHandleItemStalled, Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
				if p := lookupObserver(self); p != nil {
					p.onItemStalled(note)
				}
			}},
			{Cmd: n.selHandleAccessLog, Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
				if p := lookupObserver(self); p != nil {
					p.onAccessLog(note)
				}
			}},
			{Cmd: n.selHandleModeSwitchStart, Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
				if p := lookupObserver(self); p != nil {
					p.onModeSwitch("mode-switch-start")
				}
			}},
			{Cmd: n.selHandleModeSwitchEnd, Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
				if p := lookupObserver(self); p != nil {
					p.onModeSwitch("mode-switch-end")
				}
			}},
			{Cmd: n.selHandleModeSwitchSettings, Fn: func(self objc.ID, _ objc.SEL, _ objc.ID) {
				if p := lookupObserver(self); p != nil {
					p.onModeSwitchSettingsChanged()
				}
			}},
			{Cmd: n.selHandleAudioInterruption, Fn: func(self objc.ID, _ objc.SEL, note objc.ID) {
				if p := lookupObserver(self); p != nil {
					p.onAudioInterruption(note)
				}
			}},
			{Cmd: sel("observeValueForKeyPath:ofObject:change:context:"), Fn: func(self objc.ID, _ objc.SEL, _, _, _ objc.ID, ctx uintptr) {
				if ctx != kvoContextToken {
					return
				}
				if p := lookupObserver(self); p != nil {
					p.onTrackEvent()
				}
			}},
		})
		if err != nil {
			tvosLoadErr = fmt.Errorf("player: register MikuVideoObserver: %w", err)
			return
		}
		n.observerClass = obsCls
	})
	return tvosLoadErr
}

func lookupObserver(id objc.ID) *tvosPlayer {
	observerMu.Lock()
	p := observers[id]
	observerMu.Unlock()
	return p
}

func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(tvosNative.selStringWithUTF8, s)
}

func retainNSString(s string) objc.ID {
	id := nsString(s)
	tvosNative.getObj(id, tvosNative.selRetain)
	return id
}

func goString(ns objc.ID) string {
	if ns == 0 {
		return ""
	}
	return objc.Send[string](ns, tvosNative.selUTF8String)
}

func describeObj(obj objc.ID) string {
	if obj == 0 {
		return "<nil>"
	}
	return goString(tvosNative.getObj(obj, tvosNative.selDescription))
}

func setRetained(slot *objc.ID, val objc.ID) {
	if *slot == val {
		return
	}
	if val != 0 {
		tvosNative.getObj(val, tvosNative.selRetain)
	}
	if *slot != 0 {
		tvosNative.getObj(*slot, tvosNative.selRelease)
	}
	*slot = val
}

func setOwned(slot *objc.ID, val objc.ID) {
	if *slot != 0 {
		tvosNative.getObj(*slot, tvosNative.selRelease)
	}
	*slot = val
}

func runOnMain(fn func()) {
	n := &tvosNative
	if n.mainThread() != 0 {
		fn()
		return
	}
	pool := n.poolPush()
	defer n.poolPop(pool)
	queue := n.getObj(objc.ID(objc.GetClass("NSOperationQueue")), n.selMainQueue)
	block := objc.NewBlock(func(objc.Block) { fn() })
	defer block.Release()
	n.addOpBlock(queue, n.selAddOperationWithBlock, block)
}

func formatDescriptionIsHDR(fmtRef uintptr) bool {
	if fmtRef == 0 {
		return false
	}
	n := &tvosNative
	if tr := n.cmFmtGetExtension(fmtRef, n.kCMFmtExtTransferFunction); tr != 0 {
		if n.cfEqual(tr, n.kCMFmtTransferPQ) || n.cfEqual(tr, n.kCMFmtTransferHLG) {
			return true
		}
	}
	if alt := n.cmFmtGetExtension(fmtRef, n.kCMFmtExtAltTransferCharacteristics); alt != 0 {
		if n.cfEqual(alt, n.kCMFmtTransferPQ) || n.cfEqual(alt, n.kCMFmtTransferHLG) {
			return true
		}
	}
	codec := n.cmFmtGetMediaSubType(fmtRef)
	return codec == 0x64766831 || codec == 0x64766865 // 'dvh1', 'dvhe'
}

func backdropHasPicture(img uintptr) bool {
	n := &tvosNative
	var pixels [4 * 4 * 4]byte
	space := n.cgColorSpaceCreateRGB()
	ctx := n.cgBitmapContextCreate(unsafe.Pointer(&pixels[0]), 4, 4, 8, 16, space, cgImageAlphaPremultipliedLast)
	n.cgColorSpaceRelease(space)
	if ctx == 0 {
		return true
	}
	n.cgContextDrawImage(ctx, cgRect{W: 4, H: 4}, img)
	n.cgContextRelease(ctx)
	for i := 0; i < len(pixels); i += 4 {
		if max(pixels[i], pixels[i+1], pixels[i+2]) > 18 {
			return true
		}
	}
	return false
}

func secondsAt(t cmTime) float64 {
	if !cmTimeIsNumeric(t) {
		return 0
	}
	s := tvosNative.cmTimeGetSeconds(t)
	if math.IsNaN(s) || math.IsInf(s, 0) || s < 0 {
		return 0
	}
	return s
}

type tvosPlayer struct {
	window          objc.ID
	player          objc.ID
	view            objc.ID
	layer           objc.ID
	asset           objc.ID
	observedItem    objc.ID
	displayCriteria objc.ID
	videoOutput     objc.ID
	backdropView    objc.ID
	backdropFrame   objc.ID
	backdropContext objc.ID
	observer        objc.ID

	nextBackdropSample     float64
	nextBufferSample       float64
	backdropRadius         float64
	cachedDuration         float64
	cachedBuffer           float64
	cachedItemStatus       int64
	appliedForwardBuffer   float64
	lastTimeControlStatus  int64
	lastWaitingReason      string
	generation             uint64
	seekGeneration         uint64
	idleGeneration         uint64
	rate                   float32
	target                 float64
	seeking                bool
	foreground             bool
	fullscreen             bool
	preview                bool
	audioInterrupted       bool
	idleHeld               bool
	idleDisabled           bool
	previousIdleDisabled   bool
	matchDisplay           bool
	ended                  bool
	resolvedTracks         bool
	resolvedVideoFormat    bool
	audioActive            bool
	hdrVideo               bool
	displayCriteriaApplied bool
	destroyed              bool
	trace                  bool
	errorCode              int64

	audioMu         sync.Mutex
	audioWorking    bool
	audioDirty      bool
	audioDesired    bool
	audioForce      bool
	audioSessionSet bool
	audioSessionOn  bool
	audioClosed     bool
}

// NewNative is owned by the UIKit main thread, including its disposal.
func NewNative(window uintptr) (Engine, func()) {
	if window == 0 {
		return Engine{}, func() {}
	}
	if err := loadTVOSPlayer(); err != nil {
		log.Printf("player: native init failed: %v", err)
		return Engine{}, func() {}
	}
	p := newTVOSPlayer(objc.ID(window))
	return Engine{
		Open:         p.open,
		Seek:         p.seek,
		SetRate:      p.setRate,
		SelectAudio:  p.selectAudio,
		Snapshot:     p.snapshot,
		Stop:         p.stop,
		Fullscreen:   p.setFullscreen,
		Preview:      p.setPreview,
		Viewport:     p.setViewport,
		ViewportRect: p.setViewportRect,
		BackdropBlur: func(radius float32) bool { return p.blurBackdrop(float64(radius)) },
		Capabilities: func() Capabilities {
			return Capabilities{HDR: tvosHDRSupported(), HEVC: tvosHEVCSupported()}
		},
	}, p.destroy
}

func tvosHDRSupported() bool {
	if loadTVOSPlayer() != nil {
		return false
	}
	return tvosNative.getBool(objc.ID(objc.GetClass("AVPlayer")), tvosNative.selEligibleForHDRPlayback)
}

func tvosHEVCSupported() bool {
	if loadTVOSPlayer() != nil {
		return false
	}
	return tvosNative.vtHardwareDecode(cmVideoCodecHEVC)
}

func newTVOSPlayer(window objc.ID) *tvosPlayer {
	n := &tvosNative
	p := &tvosPlayer{
		window:                window,
		trace:                 os.Getenv("MIKUTV_NAV_TRACE") == "1",
		lastTimeControlStatus: -1,
	}

	pl := n.withObj(objc.ID(objc.GetClass("AVPlayer")), n.selPlayerWithItem, 0)
	setRetained(&p.player, pl)
	n.setBool(p.player, n.selSetAllowsExtPlayback, false)
	n.setBool(p.player, n.selSetPreventsSleep, true)

	app := n.getObj(objc.ID(objc.GetClass("UIApplication")), n.selSharedApplication)
	p.foreground = n.getInt64(app, n.selApplicationState) != uiApplicationStateBackground
	p.idleDisabled = n.getBool(app, n.selIsIdleTimerDisabled)
	if mgr := n.getObj(window, n.selAVDisplayManager); mgr != 0 {
		p.matchDisplay = n.getBool(mgr, n.selDisplayCriteriaMatching)
	}

	rootVC := n.getObj(window, n.selRootViewController)
	root := n.getObj(rootVC, n.selView)
	n.setBool(root, n.selSetOpaque, false)
	bounds := n.getRect(root, n.selBounds)

	view := n.initRect(n.getObj(objc.ID(n.layerClass), n.selAlloc), n.selInitWithFrame, bounds)
	setOwned(&p.view, view)
	n.setUintptr(p.view, n.selSetAutoresizingMask, uiViewAutoresizingFlexibleSize)
	n.setBool(p.view, n.selSetClipsToBounds, true)
	n.setBool(p.view, n.selSetUserInteract, false)
	n.setBool(p.view, n.selSetHidden, true)
	black := n.getObj(objc.ID(objc.GetClass("UIColor")), n.selBlackColor)
	n.setObj(p.view, n.selSetBackgroundColor, black)

	p.layer = n.getObj(p.view, n.selLayer)
	n.setFloat64(p.layer, n.selSetZPosition, -1)
	n.setObj(p.layer, n.selSetVideoGravity, n.avLayerVideoGravityResizeAspect)
	n.setObj(p.layer, n.selSetPlayer, p.player)
	n.insertSubview(root, n.selInsertSubviewAtIndex, p.view, 0)

	viewBounds := n.getRect(p.view, n.selBounds)
	backdrop := n.initRect(n.getObj(objc.ID(objc.GetClass("UIImageView")), n.selAlloc), n.selInitWithFrame, viewBounds)
	setOwned(&p.backdropView, backdrop)
	n.setUintptr(p.backdropView, n.selSetAutoresizingMask, uiViewAutoresizingFlexibleSize)
	n.setUintptr(p.backdropView, n.selSetContentMode, uiViewContentModeAspectFit)
	n.setObj(p.backdropView, n.selSetBackgroundColor, black)
	n.setBool(p.backdropView, n.selSetHidden, true)
	n.setObj(p.view, n.selAddSubview, p.backdropView)

	obs := n.getObj(n.getObj(objc.ID(n.observerClass), n.selAlloc), n.selInit)
	setOwned(&p.observer, obs)
	observerMu.Lock()
	observers[p.observer] = p
	observerMu.Unlock()

	center := n.getObj(objc.ID(objc.GetClass("NSNotificationCenter")), n.selDefaultCenter)
	n.addObserverSel(center, n.selAddObserver, p.observer, n.selHandleResignActive, n.uiAppWillResignActiveNotification, 0)
	n.addObserverSel(center, n.selAddObserver, p.observer, n.selHandleBecomeActive, n.uiAppDidBecomeActiveNotification, 0)
	n.addObserverSel(center, n.selAddObserver, p.observer, n.selHandleItemEnded, n.avPlayerItemDidPlayToEndNotification, 0)
	n.addObserverSel(center, n.selAddObserver, p.observer, n.selHandleItemFailed, n.avPlayerItemFailedToPlayNotification, 0)
	n.addObserverSel(center, n.selAddObserver, p.observer, n.selHandleItemStalled, n.avPlayerItemPlaybackStalledNotification, 0)
	n.addObserverSel(center, n.selAddObserver, p.observer, n.selHandleAccessLog, n.avPlayerItemNewAccessLogEntryNotification, 0)
	n.addObserverSel(center, n.selAddObserver, p.observer, n.selHandleModeSwitchStart, n.avDisplayModeSwitchStartNotification, 0)
	n.addObserverSel(center, n.selAddObserver, p.observer, n.selHandleModeSwitchEnd, n.avDisplayModeSwitchEndNotification, 0)
	n.addObserverSel(center, n.selAddObserver, p.observer, n.selHandleModeSwitchSettings, n.avDisplayModeSwitchSettingsNotification, 0)
	n.addObserverSel(center, n.selAddObserver, p.observer, n.selHandleAudioInterruption, n.avAudioSessionInterruptionNotification, 0)
	n.addKVO(p.layer, n.selAddKVO, p.observer, n.strReadyForDisplay, nsKeyValueObservingOptionNew, kvoContextToken)
	return p
}

func (p *tvosPlayer) destroy() {
	if p.destroyed {
		return
	}
	n := &tvosNative
	p.destroyed = true
	p.audioMu.Lock()
	p.audioClosed = true
	p.audioMu.Unlock()
	if p.idleHeld {
		p.idleGeneration++
		p.idleHeld = false
		app := n.getObj(objc.ID(objc.GetClass("UIApplication")), n.selSharedApplication)
		n.setBool(app, n.selSetIdleTimerDisabled, p.previousIdleDisabled)
		p.idleDisabled = p.previousIdleDisabled
	}
	p.stop()
	if p.observer != 0 {
		center := n.getObj(objc.ID(objc.GetClass("NSNotificationCenter")), n.selDefaultCenter)
		n.setObj(center, n.selRemoveObserver, p.observer)
		if p.layer != 0 {
			n.removeKVO(p.layer, n.selRemoveKVO, p.observer, n.strReadyForDisplay, kvoContextToken)
		}
		observerMu.Lock()
		delete(observers, p.observer)
		observerMu.Unlock()
		setOwned(&p.observer, 0)
	}
	if p.view != 0 {
		n.getObj(p.view, n.selRemoveFromSuperview)
	}
	setOwned(&p.backdropView, 0)
	setRetained(&p.backdropContext, 0)
	setRetained(&p.backdropFrame, 0)
	setOwned(&p.view, 0)
	setRetained(&p.player, 0)
}

func (p *tvosPlayer) refreshMatchDisplay() {
	n := &tvosNative
	if p.window == 0 {
		p.matchDisplay = false
		return
	}
	mgr := n.getObj(p.window, n.selAVDisplayManager)
	p.matchDisplay = mgr != 0 && n.getBool(mgr, n.selDisplayCriteriaMatching)
}

func (p *tvosPlayer) activateAudioSession() {
	p.setAudioSessionActive(true, false)
}

func (p *tvosPlayer) setAudioSessionActive(active, force bool) {
	p.audioMu.Lock()
	if p.audioClosed {
		p.audioMu.Unlock()
		return
	}
	if !force && !p.audioWorking && ((!p.audioSessionSet && !active) || (p.audioSessionSet && p.audioSessionOn == active)) {
		p.audioMu.Unlock()
		return
	}
	p.audioDesired = active
	if force {
		p.audioForce = true
	}
	p.audioDirty = true
	if !p.audioWorking {
		p.audioWorking = true
		go p.drainAudioSession()
	}
	p.audioMu.Unlock()
}

func (p *tvosPlayer) drainAudioSession() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	n := &tvosNative
	for {
		p.audioMu.Lock()
		if p.audioClosed || !p.audioDirty {
			p.audioWorking = false
			p.audioMu.Unlock()
			return
		}
		want := p.audioDesired
		force := p.audioForce
		alreadySame := !force && ((!p.audioSessionSet && !want) || (p.audioSessionSet && p.audioSessionOn == want))
		p.audioDirty = false
		p.audioForce = false
		p.audioMu.Unlock()
		if alreadySame {
			continue
		}
		started := time.Now()
		pool := n.poolPush()
		audio := n.getObj(objc.ID(objc.GetClass("AVAudioSession")), n.selSharedInstance)
		if audio != 0 {
			if want {
				n.setAudioCategory(audio, n.selSetCategoryMode, n.avAudioSessionCategoryPlayback, n.avAudioSessionModeMoviePlayback, 0, 0)
				n.setAudioActive(audio, n.selSetActive, true, 0)
			} else {
				n.setAudioActiveOpts(audio, n.selSetActiveWithOptions, false, avAudioSessionDeactivateNotify, 0)
			}
		}
		n.poolPop(pool)
		p.audioMu.Lock()
		p.audioSessionSet = true
		p.audioSessionOn = want
		p.audioMu.Unlock()
		if p.trace {
			log.Printf("player-audio-session: active=%t elapsed=%s", want, time.Since(started).Round(time.Microsecond))
		}
	}
}

func (p *tvosPlayer) onResignActive() {
	runOnMain(func() {
		if p.destroyed {
			return
		}
		p.foreground = false
		p.clearDisplayCriteria()
		p.applyRate()
		p.applyIdle()
	})
}

func (p *tvosPlayer) onBecomeActive() {
	runOnMain(func() {
		if p.destroyed {
			return
		}
		p.foreground = true
		p.refreshMatchDisplay()
		if p.fullscreen || p.preview {
			p.setAudioSessionActive(true, true)
		}
		p.applyDisplayCriteria()
		p.applyIdle()
		p.applyRate()
	})
}

func (p *tvosPlayer) onItemEnded(note objc.ID) {
	n := &tvosNative
	item := n.getObj(note, n.selObject)
	if item != 0 {
		n.getObj(item, n.selRetain)
	}
	runOnMain(func() {
		if item != 0 {
			defer n.getObj(item, n.selRelease)
		}
		if p.destroyed {
			return
		}
		if item == n.getObj(p.player, n.selCurrentItem) {
			p.ended = true
		}
	})
}

func (p *tvosPlayer) onItemFailed(note objc.ID) {
	n := &tvosNative
	item := n.getObj(note, n.selObject)
	if item != 0 {
		n.getObj(item, n.selRetain)
	}
	code := int64(-1)
	if info := n.getObj(note, n.selUserInfo); info != 0 {
		if errObj := n.getObjKey(info, n.selObjectForKey, n.avPlayerItemFailedErrorKey); errObj != 0 {
			if c := n.getInt64(errObj, n.selCode); c != 0 {
				code = c
			}
		}
	}
	runOnMain(func() {
		if item != 0 {
			defer n.getObj(item, n.selRelease)
		}
		if p.destroyed {
			return
		}
		if item != n.getObj(p.player, n.selCurrentItem) {
			return
		}
		p.errorCode = code
		p.cachedItemStatus = avPlayerItemStatusFailed
		p.clearDisplayCriteria()
	})
}

func (p *tvosPlayer) onItemStalled(note objc.ID) {
	if !p.trace {
		return
	}
	n := &tvosNative
	item := n.getObj(note, n.selObject)
	if item != 0 {
		n.getObj(item, n.selRetain)
	}
	runOnMain(func() {
		if item != 0 {
			defer n.getObj(item, n.selRelease)
		}
		if p.destroyed || item != n.getObj(p.player, n.selCurrentItem) {
			return
		}
		pos := secondsAt(n.getCMTime(p.player, n.selCurrentTime))
		reason := goString(n.getObj(p.player, n.selReasonForWaitingToPlay))
		log.Printf("player-stall: pos=%.2f bufEnd=%.2f bufAhead=%.2f prefBuf=%.1f reason=%q",
			pos, p.cachedBuffer, max(0, p.cachedBuffer-pos), p.appliedForwardBuffer, reason)
	})
}

func (p *tvosPlayer) onAccessLog(note objc.ID) {
	if !p.trace {
		return
	}
	n := &tvosNative
	item := n.getObj(note, n.selObject)
	if item != 0 {
		n.getObj(item, n.selRetain)
	}
	runOnMain(func() {
		if item != 0 {
			defer n.getObj(item, n.selRelease)
		}
		if p.destroyed || item != n.getObj(p.player, n.selCurrentItem) {
			return
		}
		accessLog := n.getObj(item, n.selAccessLog)
		if accessLog == 0 {
			return
		}
		events := n.getObj(accessLog, n.selEvents)
		if events == 0 {
			return
		}
		ev := n.getObj(events, n.selLastObject)
		if ev == 0 {
			return
		}
		log.Printf("player-access: stalls=%d droppedFrames=%d observedBps=%.0f indicatedBps=%.0f watched=%.1fs",
			n.getInt64(ev, n.selNumberOfStalls),
			n.getInt64(ev, n.selNumberOfDroppedVideoFrames),
			n.getFloat64(ev, n.selObservedBitrate),
			n.getFloat64(ev, n.selIndicatedBitrate),
			n.getFloat64(ev, n.selDurationWatched),
		)
	})
}

func (p *tvosPlayer) onModeSwitch(event string) {
	runOnMain(func() {
		if p.destroyed {
			return
		}
		p.refreshMatchDisplay()
		if !p.trace {
			return
		}
		n := &tvosNative
		mgr := n.getObj(p.window, n.selAVDisplayManager)
		log.Printf("player-display: %s match=%t inProgress=%t criteria=%s",
			event,
			p.matchDisplay,
			mgr != 0 && n.getBool(mgr, n.selDisplayModeSwitchInProgress),
			describeObj(n.getObj(mgr, n.selPrefDisplayCriteria)),
		)
	})
}

func (p *tvosPlayer) onModeSwitchSettingsChanged() {
	runOnMain(func() {
		if p.destroyed {
			return
		}
		p.refreshMatchDisplay()
	})
}

func (p *tvosPlayer) onAudioInterruption(note objc.ID) {
	n := &tvosNative
	var kind, options uintptr
	var hasType bool
	if info := n.getObj(note, n.selUserInfo); info != 0 {
		if typeNum := n.getObjKey(info, n.selObjectForKey, n.avAudioSessionInterruptionTypeKey); typeNum != 0 {
			kind = n.getUintptr(typeNum, n.selUnsignedIntegerValue)
			hasType = true
		}
		if optNum := n.getObjKey(info, n.selObjectForKey, n.avAudioSessionInterruptionOptionKey); optNum != 0 {
			options = n.getUintptr(optNum, n.selUnsignedIntegerValue)
		}
	}
	if !hasType {
		return
	}
	runOnMain(func() {
		if p.destroyed {
			return
		}
		switch kind {
		case avAudioSessionInterruptBegan:
			p.audioInterrupted = true
			p.applyRate()
		case avAudioSessionInterruptEnded:
			p.audioInterrupted = false
			if (p.fullscreen || p.preview) && p.foreground {
				p.setAudioSessionActive(true, true)
				if options&avAudioSessionOptShouldResume != 0 {
					p.applyRate()
				}
			}
		}
	})
}

func (p *tvosPlayer) onTrackEvent() {
	runOnMain(func() {
		if p.destroyed {
			return
		}
		n := &tvosNative
		if item := n.getObj(p.player, n.selCurrentItem); item != 0 {
			p.cachedItemStatus = n.getInt64(item, n.selStatus)
		}
		p.nextBufferSample = 0
		p.inspectTracksAndDisplayCriteria()
	})
}

func (p *tvosPlayer) observeItem(item objc.ID) {
	n := &tvosNative
	if p.observedItem == item {
		return
	}
	if p.observedItem != 0 && p.observer != 0 {
		n.removeKVO(p.observedItem, n.selRemoveKVO, p.observer, n.strStatus, kvoContextToken)
		n.removeKVO(p.observedItem, n.selRemoveKVO, p.observer, n.strTracks, kvoContextToken)
	}
	setRetained(&p.observedItem, item)
	if p.observedItem != 0 && p.observer != 0 {
		n.addKVO(p.observedItem, n.selAddKVO, p.observer, n.strStatus, nsKeyValueObservingOptionNew, kvoContextToken)
		n.addKVO(p.observedItem, n.selAddKVO, p.observer, n.strTracks, nsKeyValueObservingOptionNew, kvoContextToken)
	}
}

func (p *tvosPlayer) clearDisplayCriteria() {
	n := &tvosNative
	p.displayCriteriaApplied = false
	if p.window == 0 {
		return
	}
	if mgr := n.getObj(p.window, n.selAVDisplayManager); mgr != 0 {
		n.setObj(mgr, n.selSetPrefDisplayCriteria, 0)
	}
}

func (p *tvosPlayer) variantFrameRate() float32 {
	if p.asset == 0 {
		return 0
	}
	n := &tvosNative
	variants := n.getObj(p.asset, n.selVariants)
	if variants == 0 {
		return 0
	}
	first := n.getObj(variants, n.selFirstObject)
	if first == 0 {
		return 0
	}
	va := n.getObj(first, n.selVideoAttributes)
	if va == 0 {
		return 0
	}
	fps := n.getFloat64(va, n.selNominalFrameRate)
	if math.IsNaN(fps) || math.IsInf(fps, 0) || fps < 1.0 {
		return 0
	}
	return float32(fps)
}

func (p *tvosPlayer) resolveDisplayCriteria() (objc.ID, bool) {
	n := &tvosNative
	var assetCriteria objc.ID
	if p.asset != 0 && n.statusForKey(p.asset, n.selStatusOfValueForKey, n.strPreferredDisplayCrit, nil) == avKeyValueStatusLoaded {
		assetCriteria = n.getObj(p.asset, n.selPrefDisplayCriteria)
	}
	if item := n.getObj(p.player, n.selCurrentItem); item != 0 {
		tracks := n.getObj(item, n.selTracks)
		count := n.getUintptr(tracks, n.selCount)
		for i := range count {
			track := n.getObjIdx(tracks, n.selObjectAtIndex, i)
			if track == 0 || !n.getBool(track, n.selIsEnabled) {
				continue
			}
			source := n.getObj(track, n.selAssetTrack)
			if source == 0 {
				continue
			}
			mediaType := n.getObj(source, n.selMediaType)
			if mediaType == 0 || !n.getBoolObj(mediaType, n.selIsEqualToString, n.avMediaTypeVideo) {
				continue
			}
			descs := n.getObj(source, n.selFormatDescriptions)
			fmtObj := n.getObj(descs, n.selFirstObject)
			if fmtObj == 0 {
				continue
			}
			fps := n.getFloat32(source, n.selNominalFrameRate)
			if math.IsNaN(float64(fps)) || math.IsInf(float64(fps), 0) || fps < 1.0 {
				fps = p.variantFrameRate()
			}
			// Keep assetCriteria for SDR if track+variant did not yield a frame rate,
			// so we never downgrade a valid HLS frame-rate criteria to 0 Hz.
			if !p.hdrVideo && fps < 1.0 && assetCriteria != 0 {
				return assetCriteria, false
			}
			alloc := n.getObj(objc.ID(objc.GetClass("AVDisplayCriteria")), n.selAlloc)
			if criteria := n.initDisplayCrit(alloc, n.selInitRefreshFormat, fps, uintptr(fmtObj)); criteria != 0 {
				return criteria, true
			}
		}
	}
	if assetCriteria != 0 {
		return assetCriteria, false
	}
	return 0, false
}

func (p *tvosPlayer) applyDisplayCriteria() {
	n := &tvosNative
	item := n.getObj(p.player, n.selCurrentItem)
	if !p.foreground || !p.fullscreen || (p.asset == 0 && item == 0) {
		if p.displayCriteriaApplied {
			p.clearDisplayCriteria()
		}
		return
	}
	criteria, owned := p.resolveDisplayCriteria()
	if criteria == 0 {
		return
	}
	if p.displayCriteriaApplied && p.displayCriteria != 0 && n.getBoolObj(criteria, n.selIsEqual, p.displayCriteria) {
		if owned {
			n.getObj(criteria, n.selRelease)
		}
		return
	}
	if owned {
		setOwned(&p.displayCriteria, criteria)
	} else {
		setRetained(&p.displayCriteria, criteria)
	}
	p.displayCriteriaApplied = true
	if mgr := n.getObj(p.window, n.selAVDisplayManager); mgr != 0 {
		n.setObj(mgr, n.selSetPrefDisplayCriteria, p.displayCriteria)
		if p.trace {
			log.Printf("player-display: applied criteria=%s match=%t hdr=%t inProgress=%t",
				describeObj(p.displayCriteria),
				n.getBool(mgr, n.selDisplayCriteriaMatching),
				p.hdrVideo,
				n.getBool(mgr, n.selDisplayModeSwitchInProgress),
			)
		}
	}
}

func (p *tvosPlayer) inspectTracksAndDisplayCriteria() {
	n := &tvosNative
	item := n.getObj(p.player, n.selCurrentItem)
	if item == 0 || p.resolvedVideoFormat {
		return
	}
	readyForDisplay := n.getBool(p.layer, n.selIsReadyForDisplay)
	if !readyForDisplay && n.getInt64(item, n.selStatus) != avPlayerItemStatusReadyToPlay {
		return
	}
	tracks := n.getObj(item, n.selTracks)
	count := n.getUintptr(tracks, n.selCount)
	foundVideoFormat := false
	for i := range count {
		track := n.getObjIdx(tracks, n.selObjectAtIndex, i)
		if track == 0 || !n.getBool(track, n.selIsEnabled) {
			continue
		}
		source := n.getObj(track, n.selAssetTrack)
		if source == 0 {
			continue
		}
		mediaType := n.getObj(source, n.selMediaType)
		if mediaType == 0 {
			continue
		}
		if n.getBoolObj(mediaType, n.selIsEqualToString, n.avMediaTypeAudio) {
			p.audioActive = true
		}
		if n.getBoolObj(mediaType, n.selIsEqualToString, n.avMediaTypeVideo) {
			hdrChar := n.getBoolObj(source, n.selHasMediaCharacteristic, n.avMediaCharacteristicContainsHDRVideo)
			if hdrChar {
				p.hdrVideo = true
			}
			descs := n.getObj(source, n.selFormatDescriptions)
			if fmtObj := n.getObj(descs, n.selFirstObject); fmtObj != 0 {
				foundVideoFormat = true
				if formatDescriptionIsHDR(uintptr(fmtObj)) {
					p.hdrVideo = true
				}
				if p.trace {
					primaries := objc.ID(n.cmFmtGetExtension(uintptr(fmtObj), n.kCMFmtExtColorPrimaries))
					transfer := objc.ID(n.cmFmtGetExtension(uintptr(fmtObj), n.kCMFmtExtTransferFunction))
					var assetCrit objc.ID
					if p.asset != 0 && n.statusForKey(p.asset, n.selStatusOfValueForKey, n.strPreferredDisplayCrit, nil) == avKeyValueStatusLoaded {
						assetCrit = n.getObj(p.asset, n.selPrefDisplayCriteria)
					}
					log.Printf("player-display: track fps=%.3f variantFps=%.3f primaries=%s transfer=%s hdrChar=%t assetCriteria=%s",
						n.getFloat32(source, n.selNominalFrameRate), p.variantFrameRate(),
						describeObj(primaries), describeObj(transfer), hdrChar, describeObj(assetCrit),
					)
				}
			}
		}
	}
	if count > 0 {
		p.resolvedTracks = true
	}
	if foundVideoFormat {
		p.resolvedVideoFormat = true
		p.applyForwardBuffer(item)
		p.applyDisplayCriteria()
	}
}

func (p *tvosPlayer) applyForwardBuffer(item objc.ID) {
	if item == 0 {
		return
	}
	// Live HLS streams have an indefinite duration; let AVPlayer pick its
	// native low-latency live window instead of forcing a 30s VOD buffer.
	dur := secondsAt(tvosNative.getCMTime(item, tvosNative.selDuration))
	if dur <= 0 {
		dur = p.cachedDuration
	}
	if dur <= 0 {
		if p.appliedForwardBuffer != 0 {
			tvosNative.setFloat64(item, tvosNative.selSetPrefForwardBuffer, 0)
			p.appliedForwardBuffer = 0
		}
		return
	}
	seconds := 30.0
	if p.preview && !p.fullscreen {
		seconds = 6.0
	} else if p.hdrVideo {
		seconds = 45.0
	}
	if p.appliedForwardBuffer == seconds {
		return
	}
	tvosNative.setFloat64(item, tvosNative.selSetPrefForwardBuffer, seconds)
	p.appliedForwardBuffer = seconds
	if p.trace {
		log.Printf("player-buffer: preferredForwardBufferDuration=%.1fs dur=%.2f hdr=%t preview=%t",
			seconds, dur, p.hdrVideo, p.preview && !p.fullscreen)
	}
}

func (p *tvosPlayer) attachBackdropOutputIfSafe() {
	n := &tvosNative
	item := n.getObj(p.player, n.selCurrentItem)
	if item == 0 || p.videoOutput != 0 || p.hdrVideo {
		return
	}
	// Only attach 8-bit BGRA frame sampling to SDR items on demand (pause,
	// blur overlay, or final seconds before end). Keeping it attached during
	// active SDR playback forces continuous BGRA conversion, and attaching it
	// to HDR items forces SDR conversion and blocks HDR display mode switching.
	num := n.withUint32(objc.ID(objc.GetClass("NSNumber")), n.selNumberWithUnsignedInt, cvPixelFormat32BGRA)
	attrs := n.withObj2(objc.ID(objc.GetClass("NSDictionary")), n.selDictWithObjectForKey, num, n.kCVPixelBufferPixelFormatTypeKey)
	out := n.withObj(n.getObj(objc.ID(objc.GetClass("AVPlayerItemVideoOutput")), n.selAlloc), n.selInitWithPixelBufferAttrs, attrs)
	setOwned(&p.videoOutput, out)
	n.setObj(item, n.selAddOutput, p.videoOutput)
}

func (p *tvosPlayer) detachBackdropOutput() {
	if p.videoOutput == 0 {
		return
	}
	n := &tvosNative
	if item := n.getObj(p.player, n.selCurrentItem); item != 0 {
		n.setObj(item, n.selRemoveOutput, p.videoOutput)
	}
	setOwned(&p.videoOutput, 0)
}

func (p *tvosPlayer) isNearEnd() bool {
	if p.cachedDuration <= 0 {
		return false
	}
	n := &tvosNative
	pos := p.target
	if !p.seeking && n.getObj(p.player, n.selCurrentItem) != 0 {
		pos = secondsAt(n.getCMTime(p.player, n.selCurrentTime))
	}
	return p.cachedDuration-pos <= 5.0
}

func (p *tvosPlayer) applyRate() {
	n := &tvosNative
	if !p.foreground || (!p.fullscreen && !p.preview) || p.audioInterrupted || p.seeking || p.ended || p.rate == 0 {
		n.getObj(p.player, n.selPause)
		if p.fullscreen && !p.hdrVideo && n.getBool(p.layer, n.selIsReadyForDisplay) && !p.seeking {
			p.attachBackdropOutputIfSafe()
			p.sampleBackdropAt(n.caCurrentMediaTime())
		}
	} else if n.getObj(p.player, n.selCurrentItem) != 0 {
		if p.backdropRadius == 0 && !p.isNearEnd() {
			p.detachBackdropOutput()
		}
		n.setFloat32(p.player, n.selSetRate, p.rate)
	}
}

func (p *tvosPlayer) applyIdle() {
	n := &tvosNative
	app := n.getObj(objc.ID(objc.GetClass("UIApplication")), n.selSharedApplication)
	wanted := p.fullscreen && p.foreground
	if wanted {
		p.idleGeneration++
		if !p.idleHeld {
			p.previousIdleDisabled = n.getBool(app, n.selIsIdleTimerDisabled)
			p.idleHeld = true
		}
		n.setBool(app, n.selSetIdleTimerDisabled, true)
		p.idleDisabled = true
	} else if p.idleHeld {
		p.idleGeneration++
		generation := p.idleGeneration
		if !p.foreground {
			p.idleHeld = false
			n.setBool(app, n.selSetIdleTimerDisabled, p.previousIdleDisabled)
			p.idleDisabled = p.previousIdleDisabled
			return
		}
		// Keep idle disabled through the transaction returning to the previous page.
		queue := n.getObj(objc.ID(objc.GetClass("NSOperationQueue")), n.selMainQueue)
		block := objc.NewBlock(func(objc.Block) {
			if !p.destroyed && p.idleGeneration == generation && p.idleHeld {
				p.idleHeld = false
				n.setBool(app, n.selSetIdleTimerDisabled, p.previousIdleDisabled)
				p.idleDisabled = p.previousIdleDisabled
			}
		})
		defer block.Release()
		n.addOpBlock(queue, n.selAddOperationWithBlock, block)
	}
}

func (p *tvosPlayer) clearItem() {
	n := &tvosNative
	p.generation++
	p.seekGeneration++
	p.seeking = false
	p.ended = false
	p.resolvedTracks = false
	p.resolvedVideoFormat = false
	p.audioActive = false
	p.hdrVideo = false
	p.errorCode = 0
	p.cachedDuration = 0
	p.cachedBuffer = 0
	p.cachedItemStatus = avPlayerItemStatusUnknown
	p.appliedForwardBuffer = 0
	p.lastTimeControlStatus = -1
	p.lastWaitingReason = ""
	p.nextBufferSample = 0
	setRetained(&p.displayCriteria, 0)
	n.setBool(p.view, n.selSetHidden, true)
	n.getObj(p.player, n.selPause)
	if p.asset != 0 {
		n.getObj(p.asset, n.selCancelLoading)
		setRetained(&p.asset, 0)
	}
	p.observeItem(0)
	p.detachBackdropOutput()
	if item := n.getObj(p.player, n.selCurrentItem); item != 0 {
		n.getObj(item, n.selCancelPendingSeeks)
	}
	n.setObj(p.player, n.selReplaceCurrentItem, 0)
	p.nextBackdropSample = 0
	setRetained(&p.backdropFrame, 0)
	p.blurBackdrop(0)
}

func (p *tvosPlayer) sampleBackdropAt(hostTime float64) {
	if p.videoOutput == 0 {
		return
	}
	n := &tvosNative
	t := n.itemTimeForHost(p.videoOutput, n.selItemTimeForHostTime, hostTime)
	if !cmTimeIsNumeric(t) || !n.hasNewPixelBuffer(p.videoOutput, n.selHasNewPixelBuffer, t) {
		return
	}
	buf := n.copyPixelBuffer(p.videoOutput, n.selCopyPixelBuffer, t, 0)
	if buf != 0 {
		frame := n.withPtr(objc.ID(objc.GetClass("CIImage")), n.selImageWithCVPixelBuffer, buf)
		setRetained(&p.backdropFrame, frame)
		n.cvPixelBufferRelease(buf)
	}
}

func (p *tvosPlayer) blurBackdrop(radius float64) bool {
	n := &tvosNative
	if radius <= 0 || n.getBool(p.view, n.selIsHidden) {
		if !n.getBool(p.backdropView, n.selIsHidden) {
			n.setBool(p.backdropView, n.selSetHidden, true)
			n.setObj(p.backdropView, n.selSetImage, 0)
		}
		p.backdropRadius = 0
		if p.rate > 0 && !p.isNearEnd() {
			p.detachBackdropOutput()
		}
		return false
	}
	if p.backdropRadius == radius && !n.getBool(p.backdropView, n.selIsHidden) {
		return true
	}
	if !p.hdrVideo {
		p.attachBackdropOutputIfSafe()
		p.sampleBackdropAt(n.caCurrentMediaTime())
	}
	frame := p.backdropFrame
	width := n.getRect(p.view, n.selBounds).W
	if width <= 0 {
		return false
	}
	p.backdropRadius = radius
	if frame == 0 {
		n.setBool(p.backdropView, n.selSetHidden, true)
		n.setObj(p.backdropView, n.selSetImage, 0)
		return false
	}
	extent := n.getRect(frame, n.selExtent)
	if extent.W <= 0 || extent.H <= 0 {
		n.setBool(p.backdropView, n.selSetHidden, true)
		n.setObj(p.backdropView, n.selSetImage, 0)
		return false
	}
	scale := min(1.0, 960.0/extent.W)
	scaled := n.applyTransform(frame, n.selImageByApplyingTransform, cgAffineTransform{A: scale, D: scale})
	scaledExtent := n.getRect(scaled, n.selExtent)
	sigma := radius * scaledExtent.W / width
	clamped := n.getObj(scaled, n.selImageByClampingToExtent)
	blurred := n.blurSigma(clamped, n.selImageByApplyingGaussianBlur, sigma)
	if p.backdropContext == 0 {
		ctx := n.withObj(objc.ID(objc.GetClass("CIContext")), n.selContextWithOptions, 0)
		setRetained(&p.backdropContext, ctx)
	}
	cgImg := n.createCGImage(p.backdropContext, n.selCreateCGImageFromRect, blurred, scaledExtent)
	hasPicture := cgImg != 0 && backdropHasPicture(cgImg)
	if hasPicture {
		uiImg := n.withPtr(objc.ID(objc.GetClass("UIImage")), n.selImageWithCGImage, cgImg)
		n.setObj(p.backdropView, n.selSetImage, uiImg)
	} else {
		n.setObj(p.backdropView, n.selSetImage, 0)
	}
	if cgImg != 0 {
		n.cgImageRelease(cgImg)
	}
	n.setBool(p.backdropView, n.selSetHidden, !hasPicture)
	return hasPicture
}

func (p *tvosPlayer) open(address string, position float64) bool {
	n := &tvosNative
	urlStr := nsString(address)
	url := n.withObj(objc.ID(objc.GetClass("NSURL")), n.selURLWithString, urlStr)
	if url == 0 {
		p.clearDisplayCriteria()
		return false
	}
	p.clearItem()
	p.target = max(position, 0)
	p.audioInterrupted = false
	asset := n.withObj2(objc.ID(objc.GetClass("AVURLAsset")), n.selURLAssetWithURL, url, 0)
	if asset == 0 {
		p.clearDisplayCriteria()
		return false
	}
	setRetained(&p.asset, asset)
	generation := p.generation
	block := objc.NewBlock(func(objc.Block) {
		runOnMain(func() {
			if p.destroyed || p.generation != generation {
				return
			}
			var errObj objc.ID
			status := n.statusForKey(asset, n.selStatusOfValueForKey, n.strPlayable, &errObj)
			if status != avKeyValueStatusLoaded || !n.getBool(asset, n.selIsPlayable) {
				code := int64(-1)
				if errObj != 0 {
					if c := n.getInt64(errObj, n.selCode); c != 0 {
						code = c
					}
				}
				p.errorCode = code
				p.clearDisplayCriteria()
				return
			}
			item := n.withObj(objc.ID(objc.GetClass("AVPlayerItem")), n.selPlayerItemWithAsset, asset)
			p.applyForwardBuffer(item)
			p.observeItem(item)
			n.setObj(p.player, n.selReplaceCurrentItem, item)
			p.cachedItemStatus = n.getInt64(item, n.selStatus)
			p.nextBufferSample = 0
			p.applyDisplayCriteria()
			p.inspectTracksAndDisplayCriteria()
			n.setBool(p.view, n.selSetHidden, false)
			if p.target > 0 {
				p.seek(p.target)
			} else {
				p.applyRate()
			}
		})
	})
	defer block.Release()
	n.loadValues(asset, n.selLoadValuesAsynchronously, n.assetLoadKeys, block)
	return true
}

func (p *tvosPlayer) seek(seconds float64) {
	n := &tvosNative
	p.target = max(seconds, 0)
	p.ended = false
	p.nextBufferSample = 0
	item := n.getObj(p.player, n.selCurrentItem)
	if item == 0 {
		return
	}
	p.seeking = true
	p.seekGeneration++
	generation := p.seekGeneration
	p.applyRate()
	n.getObj(item, n.selCancelPendingSeeks)
	when := n.cmTimeMakeWithSeconds(p.target, 1000)
	block := objc.NewBlock(func(_ objc.Block, _ bool) {
		runOnMain(func() {
			if !p.destroyed && p.seekGeneration == generation {
				p.seeking = false
				p.nextBufferSample = 0
				p.applyRate()
			}
		})
	})
	defer block.Release()
	n.seekToTime(p.player, n.selSeekToTime, when, n.cmTimeZero, n.cmTimeZero, block)
}

func (p *tvosPlayer) setRate(rate float64) {
	p.rate = float32(rate)
	if rate > 0 {
		p.audioInterrupted = false
		if (p.fullscreen || p.preview) && p.foreground {
			p.activateAudioSession()
		}
	}
	p.applyRate()
}

func (p *tvosPlayer) selectAudio(name string) bool {
	if p.destroyed || p.asset == 0 || name == "" {
		return false
	}
	n := &tvosNative
	pool := n.poolPush()
	defer n.poolPop(pool)
	item := n.getObj(p.player, n.selCurrentItem)
	if item == 0 {
		return false
	}
	group := n.withObj(p.asset, n.selMediaSelectionGroup, n.avMediaCharacteristicAudible)
	if group == 0 {
		if p.trace {
			log.Printf("player-audio: no audible media selection group for %q", name)
		}
		return false
	}
	options := n.getObj(group, n.selOptions)
	if options == 0 {
		return false
	}
	count := n.getUintptr(options, n.selCount)
	target := nsString(name)
	for i := range count {
		opt := n.getObjIdx(options, n.selObjectAtIndex, i)
		if opt == 0 {
			continue
		}
		display := n.getObj(opt, n.selDisplayName)
		if display != 0 && n.getBoolObj(display, n.selIsEqualToString, target) {
			n.setObj2(item, n.selSelectMediaOption, opt, group)
			if p.trace {
				log.Printf("player-audio: selected=%s count=%d", name, count)
			}
			return true
		}
	}
	if p.trace {
		log.Printf("player-audio: option %q not found among %d options", name, count)
	}
	return false
}

func (p *tvosPlayer) snapshot() Status {
	n := &tvosNative
	item := n.getObj(p.player, n.selCurrentItem)
	position := p.target
	if !p.seeking && item != 0 {
		position = secondsAt(n.getCMTime(p.player, n.selCurrentTime))
	}
	hostTime := n.caCurrentMediaTime()
	if item != 0 && (hostTime >= p.nextBufferSample || p.seeking || p.cachedItemStatus != avPlayerItemStatusReadyToPlay) {
		p.nextBufferSample = hostTime + 0.25
		var buffer float64
		ranges := n.getObj(item, n.selLoadedTimeRanges)
		count := n.getUintptr(ranges, n.selCount)
		for i := range count {
			val := n.getObjIdx(ranges, n.selObjectAtIndex, i)
			if val == 0 {
				continue
			}
			tr := n.getCMTimeRange(val, n.selCMTimeRangeValue)
			if position >= secondsAt(tr.Start) {
				buffer = max(buffer, secondsAt(n.cmTimeRangeGetEnd(tr)))
			}
		}
		p.cachedBuffer = buffer
		p.cachedItemStatus = n.getInt64(item, n.selStatus)
		if p.cachedDuration == 0 {
			p.cachedDuration = secondsAt(n.getCMTime(item, n.selDuration))
		}
		if p.cachedDuration > 0 && p.appliedForwardBuffer == 0 {
			p.applyForwardBuffer(item)
		}
		if p.errorCode == 0 {
			if errObj := n.getObj(item, n.selError); errObj != 0 {
				p.errorCode = n.getInt64(errObj, n.selCode)
				if p.errorCode != 0 {
					p.clearDisplayCriteria()
				}
			}
		}
	}
	if !p.resolvedVideoFormat {
		p.inspectTracksAndDisplayCriteria()
	}
	readyForDisplay := n.getBool(p.layer, n.selIsReadyForDisplay)
	nearEnd := p.cachedDuration > 0 && p.cachedDuration-position <= 5.0
	if readyForDisplay && !p.seeking && p.resolvedVideoFormat && !p.hdrVideo {
		needOutput := nearEnd || p.backdropRadius > 0
		if needOutput && p.fullscreen && p.videoOutput == 0 {
			p.attachBackdropOutputIfSafe()
		}
		if p.videoOutput != 0 {
			if needOutput || p.rate == 0 {
				if hostTime >= p.nextBackdropSample {
					p.sampleBackdropAt(hostTime)
					p.nextBackdropSample = hostTime + 0.5
				}
			} else if p.rate > 0 {
				p.detachBackdropOutput()
			}
		}
	}
	itemStatus := int64(avPlayerItemStatusUnknown)
	if item != 0 {
		itemStatus = p.cachedItemStatus
	}
	tcs := n.getInt64(p.player, n.selTimeControlStatus)
	if p.trace {
		reason := ""
		if tcs == avPlayerTimeControlWaiting {
			reason = goString(n.getObj(p.player, n.selReasonForWaitingToPlay))
		}
		if tcs != p.lastTimeControlStatus || reason != p.lastWaitingReason {
			p.lastTimeControlStatus = tcs
			p.lastWaitingReason = reason
			log.Printf("player-tcs: status=%d reason=%q pos=%.2f bufAhead=%.2f seeking=%t rate=%.2f prefBuf=%.1f",
				tcs, reason, position, max(0, p.cachedBuffer-position), p.seeking, p.rate, p.appliedForwardBuffer)
		}
	}
	return Status{
		Position:            position,
		Duration:            p.cachedDuration,
		BufferEnd:           p.cachedBuffer,
		HasFrame:            readyForDisplay,
		Buffering:           item == 0 || itemStatus == avPlayerItemStatusUnknown || p.seeking || (p.rate > 0 && tcs == avPlayerTimeControlWaiting),
		Seeking:             p.seeking,
		Ended:               p.ended,
		Failed:              p.errorCode != 0 || (item != 0 && itemStatus == avPlayerItemStatusFailed),
		DisplayIncompatible: p.errorCode == avErrorNoCompatibleAlternates,
		AudioActive:         p.audioActive,
		HDRVideo:            p.hdrVideo,
		IdleDisabled:        p.idleDisabled,
		MatchDisplay:        p.matchDisplay,
		ErrorCode:           p.errorCode,
	}
}

func (p *tvosPlayer) stop() {
	p.clearItem()
	p.clearDisplayCriteria()
}

func (p *tvosPlayer) setFullscreen(active bool) {
	n := &tvosNative
	p.fullscreen = active
	if active {
		p.preview = false
		n.setObj(p.layer, n.selSetVideoGravity, n.avLayerVideoGravityResizeAspect)
		if super := n.getObj(p.view, n.selSuperview); super != 0 {
			bounds := n.getRect(super, n.selBounds)
			if n.getRect(p.view, n.selFrame) != bounds {
				n.setRect(p.view, n.selSetFrame, bounds)
			}
		}
	}
	p.applyDisplayCriteria()
	p.applyIdle()
	p.applyRate()
	if p.fullscreen {
		p.setAudioSessionActive(true, false)
	} else if !p.preview {
		p.setAudioSessionActive(false, false)
	}
}

func (p *tvosPlayer) setPreview(active bool) {
	n := &tvosNative
	p.preview = active
	if !p.fullscreen {
		if active {
			p.setAudioSessionActive(true, false)
		} else {
			n.setObj(p.layer, n.selSetVideoGravity, n.avLayerVideoGravityResizeAspect)
			if super := n.getObj(p.view, n.selSuperview); super != 0 {
				bounds := n.getRect(super, n.selBounds)
				if n.getRect(p.view, n.selFrame) != bounds {
					n.setRect(p.view, n.selSetFrame, bounds)
				}
			}
			p.setAudioSessionActive(false, false)
		}
	}
	p.applyRate()
}

func (p *tvosPlayer) setViewport(widthFraction float64) {
	n := &tvosNative
	super := n.getObj(p.view, n.selSuperview)
	if super == 0 {
		return
	}
	bounds := n.getRect(super, n.selBounds)
	want := cgRect{X: 0, Y: 0, W: bounds.W * widthFraction, H: bounds.H}
	if n.getRect(p.view, n.selFrame) != want {
		n.setRect(p.view, n.selSetFrame, want)
	}
}

func (p *tvosPlayer) setViewportRect(r ViewportRect) {
	n := &tvosNative
	super := n.getObj(p.view, n.selSuperview)
	if super == 0 {
		return
	}
	bounds := n.getRect(super, n.selBounds)
	x := max(0.0, min(bounds.W, r.X))
	y := max(0.0, min(bounds.H, r.Y))
	w := max(0.0, min(bounds.W-x, r.W))
	h := max(0.0, min(bounds.H-y, r.H))
	want := cgRect{X: x, Y: y, W: w, H: h}
	if n.getRect(p.view, n.selFrame) != want {
		n.setRect(p.view, n.selSetFrame, want)
	}
	gravity := n.avLayerVideoGravityResizeAspect
	if r.Cover {
		gravity = n.avLayerVideoGravityResizeAspectFill
	}
	n.setObj(p.layer, n.selSetVideoGravity, gravity)
}
