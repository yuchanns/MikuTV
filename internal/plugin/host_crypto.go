package plugin

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"math/big"
	"strings"

	"github.com/MoeForge/moejs"
)

const (
	maxCryptoInputBytes   = 1 << 20
	maxCryptoDecryptBytes = 4 << 20
	maxRandomBytes        = 1024
	minRSAModulusBits     = 512
	maxRSAModulusBits     = 4096
)

func hostCryptoFunctions(ctx context.Context, rt *moejs.Runtime) map[string]any {
	sha256Fn := rt.Function("sha256", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		data, err := readCryptoBytes(rt, moejs.Arg(args, 0), "utf8", maxCryptoInputBytes)
		if err != nil {
			return moejs.Undefined(), err
		}
		sum := sha256.Sum256(data)
		return rt.FromGo(hex.EncodeToString(sum[:]))
	})

	md5Fn := rt.Function("md5", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		data, err := readCryptoBytes(rt, moejs.Arg(args, 0), "utf8", maxCryptoInputBytes)
		if err != nil {
			return moejs.Undefined(), err
		}
		sum := md5.Sum(data)
		return rt.FromGo(hex.EncodeToString(sum[:]))
	})

	hmacFn := rt.Function("hmac", 3, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		algo := strings.ToLower(strings.TrimSpace(moejs.Arg(args, 0).String()))
		var factory func() hash.Hash
		switch algo {
		case "sha1":
			factory = sha1.New
		case "sha256":
			factory = sha256.New
		case "md5":
			factory = md5.New
		default:
			return moejs.Undefined(), fmt.Errorf("unsupported hmac algorithm %q", algo)
		}
		key, err := readCryptoBytes(rt, moejs.Arg(args, 1), "utf8", maxCryptoInputBytes)
		if err != nil {
			return moejs.Undefined(), fmt.Errorf("hmac key: %w", err)
		}
		data, err := readCryptoBytes(rt, moejs.Arg(args, 2), "utf8", maxCryptoInputBytes)
		if err != nil {
			return moejs.Undefined(), fmt.Errorf("hmac data: %w", err)
		}
		mac := hmac.New(factory, key)
		mac.Write(data)
		return rt.FromGo(hex.EncodeToString(mac.Sum(nil)))
	})

	aesEncryptFn := rt.Function("aesEncrypt", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		opts, err := readCryptoOptions(rt, moejs.Arg(args, 0))
		if err != nil {
			return moejs.Undefined(), err
		}
		out, encoding, err := runAESEncrypt(opts)
		if err != nil {
			return moejs.Undefined(), err
		}
		return formatCryptoOutput(rt, out, encoding)
	})

	aesDecryptFn := rt.Function("aesDecrypt", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		opts, err := readCryptoOptions(rt, moejs.Arg(args, 0))
		if err != nil {
			return moejs.Undefined(), err
		}
		out, encoding, err := runAESDecrypt(opts)
		if err != nil {
			return moejs.Undefined(), err
		}
		return formatCryptoOutput(rt, out, encoding)
	})

	rsaEncryptFn := rt.Function("rsaEncrypt", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		opts, err := readCryptoOptions(rt, moejs.Arg(args, 0))
		if err != nil {
			return moejs.Undefined(), err
		}
		out, encoding, err := runRSAEncrypt(opts)
		if err != nil {
			return moejs.Undefined(), err
		}
		return formatCryptoOutput(rt, out, encoding)
	})

	encryptLegacyFn := rt.Function("encryptRSAOAEP", 2, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		key, err := parseRSAPublicKeyPEMOrDER(moejs.Arg(args, 0).String())
		if err != nil {
			return moejs.Undefined(), err
		}
		ciphertext, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, key, []byte(moejs.Arg(args, 1).String()), nil)
		if err != nil {
			return moejs.Undefined(), err
		}
		return rt.FromGo(hex.EncodeToString(ciphertext))
	})

	randomBytesFn := rt.Function("randomBytes", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		rawN, err := rt.ToGo(moejs.Arg(args, 0))
		if err != nil {
			return moejs.Undefined(), err
		}
		n, err := toExactInt(rawN)
		if err != nil || n < 1 || n > maxRandomBytes {
			return moejs.Undefined(), fmt.Errorf("randomBytes length must be between 1 and %d", maxRandomBytes)
		}
		encoding := "hex"
		if len(args) > 1 && !args[1].IsUndefined() && !args[1].IsNull() {
			encoding = strings.ToLower(strings.TrimSpace(args[1].String()))
		}
		buf := make([]byte, n)
		if _, err := rand.Read(buf); err != nil {
			return moejs.Undefined(), err
		}
		return formatCryptoOutput(rt, buf, encoding)
	})

	return map[string]any{
		"sha256":         sha256Fn,
		"md5":            md5Fn,
		"hmac":           hmacFn,
		"aesEncrypt":     aesEncryptFn,
		"aesDecrypt":     aesDecryptFn,
		"rsaEncrypt":     rsaEncryptFn,
		"encryptRSAOAEP": encryptLegacyFn,
		"randomBytes":    randomBytesFn,
	}
}

func hostGunzipFunction(ctx context.Context, rt *moejs.Runtime) moejs.Value {
	return rt.Function("gunzip", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
		if err := ctx.Err(); err != nil {
			return moejs.Undefined(), err
		}
		raw, err := rt.ToGo(moejs.Arg(args, 0))
		if err != nil {
			return moejs.Undefined(), err
		}
		data, ok := raw.([]byte)
		if !ok || len(data) == 0 {
			return moejs.Undefined(), errors.New("byte buffer required")
		}
		if len(data) > maxInflateBytes {
			return moejs.Undefined(), errors.New("compressed payload exceeds limit")
		}
		reader, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return moejs.Undefined(), err
		}
		defer reader.Close()
		out, err := io.ReadAll(io.LimitReader(reader, maxInflateBytes+1))
		if err != nil {
			return moejs.Undefined(), err
		}
		if len(out) > maxInflateBytes {
			return moejs.Undefined(), errors.New("decompressed payload exceeds limit")
		}
		return rt.FromGo(out)
	})
}

func readCryptoOptions(rt *moejs.Runtime, val moejs.Value) (map[string]any, error) {
	if val.IsUndefined() || val.IsNull() {
		return nil, errors.New("options object required")
	}
	raw, err := rt.ToGo(val)
	if err != nil {
		return nil, err
	}
	opts, ok := raw.(map[string]any)
	if !ok || opts == nil {
		return nil, errors.New("options object required")
	}
	return opts, nil
}

func readCryptoBytes(rt *moejs.Runtime, val moejs.Value, defaultEncoding string, limit int) ([]byte, error) {
	if val.IsUndefined() || val.IsNull() {
		return nil, errors.New("input required")
	}
	raw, err := rt.ToGo(val)
	if err != nil {
		return nil, err
	}
	return decodeCryptoValue(raw, defaultEncoding, limit)
}

func decodeCryptoValue(raw any, encoding string, limit int) ([]byte, error) {
	enc := strings.ToLower(strings.TrimSpace(encoding))
	if raw == nil {
		return nil, errors.New("value required")
	}
	if buf, ok := raw.([]byte); ok {
		if enc != "" && enc != "bytes" && enc != "utf8" && enc != "base64" && enc != "hex" {
			return nil, fmt.Errorf("unsupported encoding %q", enc)
		}
		if len(buf) > limit {
			return nil, errors.New("input exceeds size limit")
		}
		out := make([]byte, len(buf))
		copy(out, buf)
		return out, nil
	}
	str, ok := raw.(string)
	if !ok {
		return nil, errors.New("string or Uint8Array required")
	}
	if enc == "" {
		enc = "utf8"
	}
	switch enc {
	case "utf8":
		if len(str) > limit {
			return nil, errors.New("input exceeds size limit")
		}
		return []byte(str), nil
	case "hex":
		cleaned := strings.TrimSpace(str)
		if len(cleaned)/2 > limit {
			return nil, errors.New("input exceeds size limit")
		}
		out, err := hex.DecodeString(cleaned)
		if err != nil {
			return nil, fmt.Errorf("invalid hex: %w", err)
		}
		if len(out) > limit {
			return nil, errors.New("input exceeds size limit")
		}
		return out, nil
	case "base64":
		cleaned := strings.TrimSpace(str)
		if base64.StdEncoding.DecodedLen(len(cleaned)) > limit+3 {
			return nil, errors.New("input exceeds size limit")
		}
		out, err := base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			out, err = base64.RawStdEncoding.DecodeString(cleaned)
			if err != nil {
				return nil, fmt.Errorf("invalid base64: %w", err)
			}
		}
		if len(out) > limit {
			return nil, errors.New("input exceeds size limit")
		}
		return out, nil
	case "bytes":
		return nil, errors.New("Uint8Array required for bytes encoding")
	default:
		return nil, fmt.Errorf("unsupported encoding %q", enc)
	}
}

func formatCryptoOutput(rt *moejs.Runtime, data []byte, encoding string) (moejs.Value, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "hex":
		return rt.FromGo(hex.EncodeToString(data))
	case "base64":
		return rt.FromGo(base64.StdEncoding.EncodeToString(data))
	case "utf8":
		return rt.FromGo(string(data))
	case "bytes":
		return rt.FromGo(data)
	default:
		return moejs.Undefined(), fmt.Errorf("unsupported outputEncoding %q", encoding)
	}
}

func optString(opts map[string]any, key string) string {
	v, ok := opts[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func runAESEncrypt(opts map[string]any) ([]byte, string, error) {
	mode := strings.ToLower(strings.TrimSpace(optString(opts, "mode")))
	keyEnc := optString(opts, "keyEncoding")
	if keyEnc == "" {
		keyEnc = "utf8"
	}
	ivEnc := optString(opts, "ivEncoding")
	if ivEnc == "" {
		ivEnc = "utf8"
	}
	inEnc := optString(opts, "inputEncoding")
	if inEnc == "" {
		inEnc = "utf8"
	}
	outEnc := strings.ToLower(strings.TrimSpace(optString(opts, "outputEncoding")))
	if outEnc == "" {
		outEnc = "base64"
	}
	if outEnc != "base64" && outEnc != "hex" && outEnc != "bytes" {
		return nil, "", fmt.Errorf("unsupported outputEncoding %q", outEnc)
	}

	key, err := decodeCryptoValue(opts["key"], keyEnc, 64)
	if err != nil {
		return nil, "", fmt.Errorf("aes key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", err
	}
	plaintext, err := decodeCryptoValue(opts["data"], inEnc, maxCryptoInputBytes)
	if err != nil {
		return nil, "", fmt.Errorf("aes data: %w", err)
	}
	padding := strings.ToLower(strings.TrimSpace(optString(opts, "padding")))

	switch mode {
	case "cbc":
		iv, err := decodeCryptoValue(opts["iv"], ivEnc, aes.BlockSize)
		if err != nil || len(iv) != aes.BlockSize {
			return nil, "", errors.New("aes-cbc requires 16-byte iv")
		}
		padded, err := applyAESPadding(plaintext, padding)
		if err != nil {
			return nil, "", err
		}
		out := make([]byte, len(padded))
		cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
		return out, outEnc, nil

	case "ecb":
		padded, err := applyAESPadding(plaintext, padding)
		if err != nil {
			return nil, "", err
		}
		out := make([]byte, len(padded))
		for i := 0; i < len(padded); i += aes.BlockSize {
			block.Encrypt(out[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
		}
		return out, outEnc, nil

	case "ctr":
		if padding != "" && padding != "none" {
			return nil, "", errors.New("aes-ctr does not use padding")
		}
		iv, err := decodeCryptoValue(opts["iv"], ivEnc, aes.BlockSize)
		if err != nil || len(iv) != aes.BlockSize {
			return nil, "", errors.New("aes-ctr requires 16-byte iv")
		}
		out := make([]byte, len(plaintext))
		cipher.NewCTR(block, iv).XORKeyStream(out, plaintext)
		return out, outEnc, nil

	case "gcm":
		if padding != "" && padding != "none" {
			return nil, "", errors.New("aes-gcm does not use padding")
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, "", err
		}
		iv, err := decodeCryptoValue(opts["iv"], ivEnc, aead.NonceSize())
		if err != nil || len(iv) != aead.NonceSize() {
			return nil, "", fmt.Errorf("aes-gcm requires %d-byte iv", aead.NonceSize())
		}
		var aad []byte
		if rawAAD, ok := opts["aad"]; ok && rawAAD != nil {
			aadEnc := optString(opts, "aadEncoding")
			if aadEnc == "" {
				aadEnc = "utf8"
			}
			aad, err = decodeCryptoValue(rawAAD, aadEnc, maxCryptoInputBytes)
			if err != nil {
				return nil, "", fmt.Errorf("aes-gcm aad: %w", err)
			}
		}
		out := aead.Seal(nil, iv, plaintext, aad)
		return out, outEnc, nil

	default:
		return nil, "", fmt.Errorf("unsupported aes mode %q", mode)
	}
}

func runAESDecrypt(opts map[string]any) ([]byte, string, error) {
	mode := strings.ToLower(strings.TrimSpace(optString(opts, "mode")))
	keyEnc := optString(opts, "keyEncoding")
	if keyEnc == "" {
		keyEnc = "utf8"
	}
	ivEnc := optString(opts, "ivEncoding")
	if ivEnc == "" {
		ivEnc = "utf8"
	}
	inEnc := optString(opts, "inputEncoding")
	if inEnc == "" {
		inEnc = "base64"
	}
	outEnc := strings.ToLower(strings.TrimSpace(optString(opts, "outputEncoding")))
	if outEnc == "" {
		outEnc = "utf8"
	}
	if outEnc != "utf8" && outEnc != "hex" && outEnc != "base64" && outEnc != "bytes" {
		return nil, "", fmt.Errorf("unsupported outputEncoding %q", outEnc)
	}

	key, err := decodeCryptoValue(opts["key"], keyEnc, 64)
	if err != nil {
		return nil, "", fmt.Errorf("aes key: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, "", err
	}
	ciphertext, err := decodeCryptoValue(opts["data"], inEnc, maxCryptoDecryptBytes)
	if err != nil {
		return nil, "", fmt.Errorf("aes data: %w", err)
	}
	padding := strings.ToLower(strings.TrimSpace(optString(opts, "padding")))

	switch mode {
	case "cbc":
		iv, err := decodeCryptoValue(opts["iv"], ivEnc, aes.BlockSize)
		if err != nil || len(iv) != aes.BlockSize {
			return nil, "", errors.New("aes-cbc requires 16-byte iv")
		}
		if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
			return nil, "", errors.New("aes-cbc ciphertext must be a non-empty multiple of 16 bytes")
		}
		plain := make([]byte, len(ciphertext))
		cipher.NewCBCDecrypter(block, iv).CryptBlocks(plain, ciphertext)
		out, err := stripAESPadding(plain, padding)
		return out, outEnc, err

	case "ecb":
		if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
			return nil, "", errors.New("aes-ecb ciphertext must be a non-empty multiple of 16 bytes")
		}
		plain := make([]byte, len(ciphertext))
		for i := 0; i < len(ciphertext); i += aes.BlockSize {
			block.Decrypt(plain[i:i+aes.BlockSize], ciphertext[i:i+aes.BlockSize])
		}
		out, err := stripAESPadding(plain, padding)
		return out, outEnc, err

	case "ctr":
		if padding != "" && padding != "none" {
			return nil, "", errors.New("aes-ctr does not use padding")
		}
		iv, err := decodeCryptoValue(opts["iv"], ivEnc, aes.BlockSize)
		if err != nil || len(iv) != aes.BlockSize {
			return nil, "", errors.New("aes-ctr requires 16-byte iv")
		}
		out := make([]byte, len(ciphertext))
		cipher.NewCTR(block, iv).XORKeyStream(out, ciphertext)
		return out, outEnc, nil

	case "gcm":
		if padding != "" && padding != "none" {
			return nil, "", errors.New("aes-gcm does not use padding")
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return nil, "", err
		}
		iv, err := decodeCryptoValue(opts["iv"], ivEnc, aead.NonceSize())
		if err != nil || len(iv) != aead.NonceSize() {
			return nil, "", fmt.Errorf("aes-gcm requires %d-byte iv", aead.NonceSize())
		}
		var aad []byte
		if rawAAD, ok := opts["aad"]; ok && rawAAD != nil {
			aadEnc := optString(opts, "aadEncoding")
			if aadEnc == "" {
				aadEnc = "utf8"
			}
			aad, err = decodeCryptoValue(rawAAD, aadEnc, maxCryptoInputBytes)
			if err != nil {
				return nil, "", fmt.Errorf("aes-gcm aad: %w", err)
			}
		}
		out, err := aead.Open(nil, iv, ciphertext, aad)
		if err != nil {
			return nil, "", errors.New("aes-gcm authentication failed")
		}
		return out, outEnc, nil

	default:
		return nil, "", fmt.Errorf("unsupported aes mode %q", mode)
	}
}

func applyAESPadding(data []byte, padding string) ([]byte, error) {
	switch padding {
	case "", "pkcs7":
		padLen := aes.BlockSize - (len(data) % aes.BlockSize)
		out := make([]byte, len(data)+padLen)
		copy(out, data)
		for i := len(data); i < len(out); i++ {
			out[i] = byte(padLen)
		}
		return out, nil
	case "none":
		if len(data) == 0 || len(data)%aes.BlockSize != 0 {
			return nil, errors.New("aes with padding=none requires non-empty 16-byte aligned input")
		}
		out := make([]byte, len(data))
		copy(out, data)
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported padding %q", padding)
	}
}

func stripAESPadding(data []byte, padding string) ([]byte, error) {
	switch padding {
	case "", "pkcs7":
		if len(data) == 0 || len(data)%aes.BlockSize != 0 {
			return nil, errors.New("invalid pkcs7 padded block")
		}
		padLen := int(data[len(data)-1])
		if padLen < 1 || padLen > aes.BlockSize || padLen > len(data) {
			return nil, errors.New("invalid pkcs7 padding")
		}
		var bad byte
		for i := len(data) - padLen; i < len(data); i++ {
			bad |= data[i] ^ byte(padLen)
		}
		if bad != 0 {
			return nil, errors.New("invalid pkcs7 padding")
		}
		return data[:len(data)-padLen], nil
	case "none":
		return data, nil
	default:
		return nil, fmt.Errorf("unsupported padding %q", padding)
	}
}

func runRSAEncrypt(opts map[string]any) ([]byte, string, error) {
	mode := strings.ToLower(strings.TrimSpace(optString(opts, "mode")))
	if mode == "" {
		mode = "oaep-sha256"
	}
	inEnc := optString(opts, "inputEncoding")
	if inEnc == "" {
		inEnc = "utf8"
	}
	outEnc := strings.ToLower(strings.TrimSpace(optString(opts, "outputEncoding")))
	if outEnc == "" {
		outEnc = "hex"
	}
	if outEnc != "hex" && outEnc != "base64" && outEnc != "bytes" {
		return nil, "", fmt.Errorf("unsupported outputEncoding %q", outEnc)
	}
	data, err := decodeCryptoValue(opts["data"], inEnc, maxCryptoInputBytes)
	if err != nil {
		return nil, "", fmt.Errorf("rsa data: %w", err)
	}

	var modN, expE *big.Int
	var pubKey *rsa.PublicKey
	pemStr := strings.TrimSpace(optString(opts, "pem"))
	modHex := strings.TrimSpace(optString(opts, "modulusHex"))
	expHex := strings.TrimSpace(optString(opts, "exponentHex"))

	switch {
	case pemStr != "":
		if modHex != "" || expHex != "" {
			return nil, "", errors.New("specify either pem or modulusHex/exponentHex")
		}
		pubKey, err = parseRSAPublicKeyPEMOrDER(pemStr)
		if err != nil {
			return nil, "", err
		}
		modN = pubKey.N
		expE = big.NewInt(int64(pubKey.E))
	case modHex != "" && expHex != "":
		modBytes, err := decodeHexBigInt(modHex)
		if err != nil {
			return nil, "", fmt.Errorf("rsa modulusHex: %w", err)
		}
		expBytes, err := decodeHexBigInt(expHex)
		if err != nil {
			return nil, "", fmt.Errorf("rsa exponentHex: %w", err)
		}
		modN = new(big.Int).SetBytes(modBytes)
		expE = new(big.Int).SetBytes(expBytes)
		bits := modN.BitLen()
		if bits < minRSAModulusBits || bits > maxRSAModulusBits {
			return nil, "", fmt.Errorf("rsa modulus size must be between %d and %d bits", minRSAModulusBits, maxRSAModulusBits)
		}
		if expE.Sign() <= 0 || expE.BitLen() > maxRSAModulusBits {
			return nil, "", errors.New("rsa exponent out of range")
		}
		if mode != "raw" {
			if !expE.IsInt64() || expE.Int64() < 3 || expE.Int64() > math.MaxInt32 {
				return nil, "", errors.New("rsa exponent out of range")
			}
			pubKey = &rsa.PublicKey{N: modN, E: int(expE.Int64())}
		}
	default:
		return nil, "", errors.New("rsa key required (pem or modulusHex + exponentHex)")
	}

	switch mode {
	case "oaep-sha256":
		out, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, pubKey, data, nil)
		return out, outEnc, err
	case "pkcs1v15":
		out, err := rsa.EncryptPKCS1v15(rand.Reader, pubKey, data)
		return out, outEnc, err
	case "raw":
		blockSize := (modN.BitLen() + 7) / 8
		if len(data) > blockSize {
			return nil, "", errors.New("rsa raw input exceeds modulus byte length")
		}
		m := new(big.Int).SetBytes(data)
		if m.Cmp(modN) >= 0 {
			return nil, "", errors.New("rsa raw message must be less than modulus")
		}
		c := new(big.Int).Exp(m, expE, modN)
		out := make([]byte, blockSize)
		c.FillBytes(out)
		return out, outEnc, nil
	default:
		return nil, "", fmt.Errorf("unsupported rsa mode %q", mode)
	}
}

func parseRSAPublicKeyPEMOrDER(raw string) (*rsa.PublicKey, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, errors.New("empty RSA key")
	}
	var der []byte
	var blockType string
	if block, _ := pem.Decode([]byte(trimmed)); block != nil {
		der = block.Bytes
		blockType = block.Type
	} else {
		cleaned := strings.NewReplacer("\r", "", "\n", "", " ", "", "\t", "").Replace(trimmed)
		var err error
		der, err = base64.StdEncoding.DecodeString(cleaned)
		if err != nil {
			return nil, err
		}
	}
	var key *rsa.PublicKey
	if blockType == "RSA PUBLIC KEY" {
		parsed, err := x509.ParsePKCS1PublicKey(der)
		if err != nil {
			return nil, err
		}
		key = parsed
	} else if parsed, err := x509.ParsePKIXPublicKey(der); err == nil {
		rsaKey, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("RSA public key required")
		}
		key = rsaKey
	} else if pkcs1, err1 := x509.ParsePKCS1PublicKey(der); err1 == nil {
		key = pkcs1
	} else {
		return nil, err
	}
	if bits := key.N.BitLen(); bits < minRSAModulusBits || bits > maxRSAModulusBits {
		return nil, fmt.Errorf("rsa modulus size must be between %d and %d bits", minRSAModulusBits, maxRSAModulusBits)
	}
	return key, nil
}

func decodeHexBigInt(s string) ([]byte, error) {
	cleaned := strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "0x"), "0X")
	if cleaned == "" {
		return nil, errors.New("empty hex")
	}
	if len(cleaned)%2 == 1 {
		cleaned = "0" + cleaned
	}
	return hex.DecodeString(cleaned)
}

func toExactInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case float64:
		if math.IsNaN(n) || math.IsInf(n, 0) || math.Trunc(n) != n {
			return 0, errors.New("integer required")
		}
		return int(n), nil
	default:
		return 0, errors.New("integer required")
	}
}
