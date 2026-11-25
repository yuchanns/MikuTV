package plugin

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"strings"
	"sync"
	"testing"

	"github.com/MoeForge/moejs"
)

func newTestHostRuntime(t *testing.T, ctx context.Context) *moejs.Runtime {
	t.Helper()
	rt := moejs.NewRuntime(moejs.Options{MemoryLimit: 64 << 20, DisableDynamicCode: true})
	store, err := NewStorage(nil, nil)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	if err := installHost(ctx, rt, store); err != nil {
		t.Fatalf("installHost: %v", err)
	}
	return rt
}

func evalJS(rt *moejs.Runtime, src string) (moejs.Value, error) {
	script, err := moejs.CompileScript("test.js", src)
	if err != nil {
		return moejs.Undefined(), err
	}
	return rt.RunScript(script)
}

func evalString(t *testing.T, rt *moejs.Runtime, src string) string {
	t.Helper()
	val, err := evalJS(rt, src)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return val.String()
}

func TestHostCryptoDigestsAndHMACVectors(t *testing.T) {
	rt := newTestHostRuntime(t, context.Background())

	// RFC 1321 MD5 test vectors
	if got := evalString(t, rt, `host.crypto.md5("")`); got != "d41d8cd98f00b204e9800998ecf8427e" {
		t.Fatalf("md5('') = %q", got)
	}
	if got := evalString(t, rt, `host.crypto.md5("abc")`); got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatalf("md5('abc') = %q", got)
	}
	if got := evalString(t, rt, `host.crypto.md5(new Uint8Array([0x61, 0x62, 0x63]))`); got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Fatalf("md5(Uint8Array) = %q", got)
	}

	// FIPS 180-4 SHA-256 test vectors (string + Uint8Array)
	const wantSHA = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if got := evalString(t, rt, `host.crypto.sha256("abc")`); got != wantSHA {
		t.Fatalf("sha256('abc') = %q", got)
	}
	if got := evalString(t, rt, `host.crypto.sha256(new Uint8Array([0x61, 0x62, 0x63]))`); got != wantSHA {
		t.Fatalf("sha256(Uint8Array) = %q", got)
	}

	// RFC 2202 / RFC 4231 HMAC vectors ("Jefe" / "what do ya want for nothing?")
	if got := evalString(t, rt, `host.crypto.hmac("md5", "Jefe", "what do ya want for nothing?")`); got != "750c783e6ab0b503eaa86e310a5db738" {
		t.Fatalf("hmac-md5 = %q", got)
	}
	if got := evalString(t, rt, `host.crypto.hmac("sha1", "Jefe", "what do ya want for nothing?")`); got != "effcdf6ae5eb2fa2d27416d5f184df9c259a7c79" {
		t.Fatalf("hmac-sha1 = %q", got)
	}
	if got := evalString(t, rt, `host.crypto.hmac("sha256", "Jefe", "what do ya want for nothing?")`); got != "5bdcc146bf60754e6a042426089575c75a003f089d2739839dec58b964ec3843" {
		t.Fatalf("hmac-sha256 = %q", got)
	}
	if _, err := evalJS(rt, `host.crypto.hmac("sha512", "k", "d")`); err == nil {
		t.Fatal("expected unsupported hmac algorithm error")
	}
}

func TestHostCryptoAESModesAndValidation(t *testing.T) {
	rt := newTestHostRuntime(t, context.Background())

	// NIST SP 800-38A AES-128-ECB single block (padding: "none")
	ecbGot := evalString(t, rt, `host.crypto.aesEncrypt({
		mode: "ecb",
		key: "2b7e151628aed2a6abf7158809cf4f3c",
		keyEncoding: "hex",
		data: "6bc1bee22e409f96e93d7e117393172a",
		inputEncoding: "hex",
		outputEncoding: "hex",
		padding: "none"
	})`)
	if ecbGot != "3ad77bb40d7a3660a89ecaf32466ef97" {
		t.Fatalf("NIST AES-128-ECB mismatch: got %q", ecbGot)
	}

	// AES-128-CBC round-trip (PKCS#7, UTF-8 key/iv, base64 default)
	cbcPlain := evalString(t, rt, `(() => {
		const ct = host.crypto.aesEncrypt({
			mode: "cbc",
			key: "0123456789abcdef",
			iv: "abcdef0123456789",
			data: "hello audio stage 唱片"
		});
		return host.crypto.aesDecrypt({
			mode: "cbc",
			key: "0123456789abcdef",
			iv: "abcdef0123456789",
			data: ct
		});
	})()`)
	if cbcPlain != "hello audio stage 唱片" {
		t.Fatalf("AES-CBC round-trip = %q", cbcPlain)
	}

	// AES-128-ECB with PKCS#7 round-trip (hex output)
	ecbPlain := evalString(t, rt, `(() => {
		const ct = host.crypto.aesEncrypt({
			mode: "ecb",
			key: "0123456789abcdef",
			data: "short-msg",
			outputEncoding: "hex"
		});
		return host.crypto.aesDecrypt({
			mode: "ecb",
			key: "0123456789abcdef",
			data: ct,
			inputEncoding: "hex",
			outputEncoding: "utf8"
		});
	})()`)
	if ecbPlain != "short-msg" {
		t.Fatalf("AES-ECB round-trip = %q", ecbPlain)
	}

	// AES-256-CTR round-trip using Uint8Array bytes output
	ctrPlain := evalString(t, rt, `(() => {
		const key = host.crypto.randomBytes(32, "bytes");
		const iv = host.crypto.randomBytes(16, "bytes");
		const ct = host.crypto.aesEncrypt({
			mode: "ctr",
			key,
			iv,
			data: "stream-counter-payload",
			outputEncoding: "bytes"
		});
		return host.crypto.aesDecrypt({
			mode: "ctr",
			key,
			iv,
			data: ct
		});
	})()`)
	if ctrPlain != "stream-counter-payload" {
		t.Fatalf("AES-CTR round-trip = %q", ctrPlain)
	}

	// AES-256-GCM with AAD + tamper rejection
	gcmPlain := evalString(t, rt, `(() => {
		const key = "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff";
		const iv = "0102030405060708090a0b0c";
		const ct = host.crypto.aesEncrypt({
			mode: "gcm",
			key, keyEncoding: "hex",
			iv, ivEncoding: "hex",
			aad: "header-v1",
			data: "authenticated-secret"
		});
		return host.crypto.aesDecrypt({
			mode: "gcm",
			key, keyEncoding: "hex",
			iv, ivEncoding: "hex",
			aad: "header-v1",
			data: ct
		});
	})()`)
	if gcmPlain != "authenticated-secret" {
		t.Fatalf("AES-GCM round-trip = %q", gcmPlain)
	}

	// Bad key, bad IV, bad padding, tampered GCM AAD must all fail
	invalidCases := []string{
		`host.crypto.aesEncrypt({ mode: "cbc", key: "short", iv: "0123456789abcdef", data: "x" })`,
		`host.crypto.aesEncrypt({ mode: "cbc", key: "0123456789abcdef", iv: "short", data: "x" })`,
		`host.crypto.aesEncrypt({ mode: "ecb", key: "0123456789abcdef", data: "not-16-bytes", padding: "none" })`,
		`host.crypto.aesDecrypt({ mode: "cbc", key: "0123456789abcdef", iv: "0123456789abcdef", data: "001122", inputEncoding: "hex" })`,
		`host.crypto.aesDecrypt({ mode: "ecb", key: "0123456789abcdef", data: "00000000000000000000000000000000", inputEncoding: "hex" })`,
		`(() => {
			const key = "00112233445566778899aabbccddeeff";
			const iv = "0102030405060708090a0b0c";
			const ct = host.crypto.aesEncrypt({ mode: "gcm", key, keyEncoding: "hex", iv, ivEncoding: "hex", aad: "a", data: "hi" });
			return host.crypto.aesDecrypt({ mode: "gcm", key, keyEncoding: "hex", iv, ivEncoding: "hex", aad: "b", data: ct });
		})()`,
	}
	for i, expr := range invalidCases {
		if _, err := evalJS(rt, expr); err == nil {
			t.Fatalf("case %d: expected error for invalid AES call", i)
		}
	}
}

func TestHostCryptoRSAModesAndLegacyCompat(t *testing.T) {
	priv, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pemBlock := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	derB64 := base64.StdEncoding.EncodeToString(der)
	modHex := hex.EncodeToString(priv.N.Bytes())
	expHex := hex.EncodeToString(big.NewInt(int64(priv.E)).Bytes())

	rt := newTestHostRuntime(t, context.Background())
	if err := rt.SetGlobal("testPEM", pemBlock); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetGlobal("testDER", derB64); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetGlobal("testMod", modHex); err != nil {
		t.Fatal(err)
	}
	if err := rt.SetGlobal("testExp", expHex); err != nil {
		t.Fatal(err)
	}

	// 1. Legacy host.crypto.encryptRSAOAEP compatibility
	legacyHex := evalString(t, rt, `host.crypto.encryptRSAOAEP(testDER, "legacy-secret")`)
	legacyCT, err := hex.DecodeString(legacyHex)
	if err != nil {
		t.Fatalf("legacy hex decode: %v", err)
	}
	plain, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, legacyCT, nil)
	if err != nil || string(plain) != "legacy-secret" {
		t.Fatalf("legacy OAEP decrypt: %q err=%v", plain, err)
	}

	// 2. rsaEncrypt oaep-sha256 with PEM
	oaepHex := evalString(t, rt, `host.crypto.rsaEncrypt({ mode: "oaep-sha256", pem: testPEM, data: "oaep-msg" })`)
	oaepCT, err := hex.DecodeString(oaepHex)
	if err != nil {
		t.Fatalf("oaep hex decode: %v", err)
	}
	plain, err = rsa.DecryptOAEP(sha256.New(), rand.Reader, priv, oaepCT, nil)
	if err != nil || string(plain) != "oaep-msg" {
		t.Fatalf("OAEP decrypt: %q err=%v", plain, err)
	}

	// 3. rsaEncrypt pkcs1v15 with modulusHex + exponentHex
	v15B64 := evalString(t, rt, `host.crypto.rsaEncrypt({ mode: "pkcs1v15", modulusHex: testMod, exponentHex: testExp, data: "v15-msg", outputEncoding: "base64" })`)
	v15CT, err := base64.StdEncoding.DecodeString(v15B64)
	if err != nil {
		t.Fatalf("v15 base64 decode: %v", err)
	}
	plain, err = rsa.DecryptPKCS1v15(rand.Reader, priv, v15CT)
	if err != nil || string(plain) != "v15-msg" {
		t.Fatalf("PKCS1v15 decrypt: %q err=%v", plain, err)
	}

	// 4. rsaEncrypt raw modular exponentiation (fixed-width modulus bytes)
	rawHex := evalString(t, rt, `host.crypto.rsaEncrypt({ mode: "raw", modulusHex: testMod, exponentHex: testExp, data: "0123456789abcdef" })`)
	rawCT, err := hex.DecodeString(rawHex)
	if err != nil {
		t.Fatalf("raw hex decode: %v", err)
	}
	if len(rawCT) != (priv.N.BitLen()+7)/8 {
		t.Fatalf("raw output width = %d, want %d", len(rawCT), (priv.N.BitLen()+7)/8)
	}
	recovered := new(big.Int).Exp(new(big.Int).SetBytes(rawCT), priv.D, priv.N)
	if string(recovered.Bytes()) != "0123456789abcdef" {
		t.Fatalf("raw RSA round-trip mismatch: got %q", string(recovered.Bytes()))
	}

	// Reject undersized modulus (<512 bits) and oversized raw input
	if _, err := evalJS(rt, `host.crypto.rsaEncrypt({ mode: "raw", modulusHex: "00ff", exponentHex: "010001", data: "hi" })`); err == nil {
		t.Fatal("expected error for tiny RSA modulus")
	}
	if _, err := evalJS(rt, `host.crypto.rsaEncrypt({ mode: "raw", modulusHex: testMod, exponentHex: testExp, data: "a".repeat(200) })`); err == nil {
		t.Fatal("expected error for raw input longer than modulus")
	}
}

func TestHostRandomBytesAndGunzipLimits(t *testing.T) {
	rt := newTestHostRuntime(t, context.Background())

	hex16 := evalString(t, rt, `host.crypto.randomBytes(16)`)
	if len(hex16) != 32 {
		t.Fatalf("randomBytes(16) hex len = %d, want 32", len(hex16))
	}
	if _, err := hex.DecodeString(hex16); err != nil {
		t.Fatalf("randomBytes(16) invalid hex: %v", err)
	}
	if _, err := evalJS(rt, `host.crypto.randomBytes(0)`); err == nil {
		t.Fatal("expected error for randomBytes(0)")
	}
	if _, err := evalJS(rt, `host.crypto.randomBytes(2048)`); err == nil {
		t.Fatal("expected error for randomBytes(2048)")
	}

	// Valid gzip decompression
	var gzBuf bytes.Buffer
	gw := gzip.NewWriter(&gzBuf)
	_, _ = gw.Write([]byte("compressed-track-metadata"))
	_ = gw.Close()
	if err := rt.SetGlobal("gzSample", gzBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	val, err := evalJS(rt, `host.compress.gunzip(gzSample)`)
	if err != nil {
		t.Fatalf("gunzip valid: %v", err)
	}
	gotRaw, err := rt.ToGo(val)
	if err != nil {
		t.Fatalf("ToGo gunzip: %v", err)
	}
	if gotBytes, ok := gotRaw.([]byte); !ok || string(gotBytes) != "compressed-track-metadata" {
		t.Fatalf("gunzip mismatch: %T %q", gotRaw, gotRaw)
	}

	// Gzip bomb exceeding maxInflateBytes must be rejected
	var bombBuf bytes.Buffer
	bw := gzip.NewWriter(&bombBuf)
	_, _ = bw.Write(bytes.Repeat([]byte("A"), maxInflateBytes+1024))
	_ = bw.Close()
	if err := rt.SetGlobal("gzBomb", bombBuf.Bytes()); err != nil {
		t.Fatal(err)
	}
	if _, err := evalJS(rt, `host.compress.gunzip(gzBomb)`); err == nil {
		t.Fatal("expected error for gzip bomb exceeding maxInflateBytes")
	}

	// Oversized crypto input rejected
	if err := rt.SetGlobal("hugeStr", strings.Repeat("x", maxCryptoInputBytes+1)); err != nil {
		t.Fatal(err)
	}
	if _, err := evalJS(rt, `host.crypto.md5(hugeStr)`); err == nil {
		t.Fatal("expected error for oversized md5 input")
	}
}

func TestHostCryptoCanceledContextAndRace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rt := newTestHostRuntime(t, ctx)
	for _, expr := range []string{
		`host.crypto.md5("a")`,
		`host.crypto.sha256("a")`,
		`host.crypto.hmac("sha256", "k", "v")`,
		`host.crypto.randomBytes(16)`,
		`host.compress.gunzip(new Uint8Array([1, 2]))`,
	} {
		if _, err := evalJS(rt, expr); err == nil {
			t.Fatalf("expected canceled context error for %s", expr)
		}
	}

	// Concurrent independent runtimes under -race
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			localRT := newTestHostRuntime(t, context.Background())
			if got := evalString(t, localRT, `host.crypto.hmac("sha256", "key", "msg")`); len(got) != 64 {
				t.Errorf("unexpected hmac len: %d", len(got))
			}
		}()
	}
	wg.Wait()
}
