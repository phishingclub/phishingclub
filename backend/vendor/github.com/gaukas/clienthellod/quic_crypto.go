package clienthellod

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"errors"

	"golang.org/x/crypto/cryptobyte"
	"golang.org/x/crypto/hkdf"
)

func ClientInitialKeysCalc(initialRandom []byte) (clientKey, clientIV, clientHpKey []byte, err error) {
	initialSalt := []byte{
		0x38, 0x76, 0x2c, 0xf7,
		0xf5, 0x59, 0x34, 0xb3,
		0x4d, 0x17, 0x9a, 0xe6,
		0xa4, 0xc8, 0x0c, 0xad,
		0xcc, 0xbb, 0x7f, 0x0a,
	} // magic value, the first SHA-1 collision

	initialSecret := hkdf.Extract(sha256.New, initialRandom, initialSalt)

	clientSecret, err := hkdfExpandLabel(initialSecret, "client in", nil, 32)
	if err != nil {
		return nil, nil, nil, err
	}
	clientKey, err = hkdfExpandLabel(clientSecret, "quic key", nil, 16)
	if err != nil {
		return nil, nil, nil, err
	}
	clientIV, err = hkdfExpandLabel(clientSecret, "quic iv", nil, 12)
	if err != nil {
		return nil, nil, nil, err
	}
	clientHpKey, err = hkdfExpandLabel(clientSecret, "quic hp", nil, 16)
	if err != nil {
		return nil, nil, nil, err
	}

	return
}

func hkdfExpandLabel(key []byte, label string, context []byte, length uint16) ([]byte, error) {
	// see https://tools.ietf.org/html/rfc8446#section-7.1
	// code from crypto/tls
	var hkdfLabel cryptobyte.Builder
	hkdfLabel.AddUint16(uint16(length))
	hkdfLabel.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddBytes([]byte("tls13 "))
		b.AddBytes([]byte(label))
	})
	hkdfLabel.AddUint8LengthPrefixed(func(b *cryptobyte.Builder) {
		b.AddBytes(context)
	})
	hkdfLabelBytes, err := hkdfLabel.Bytes()
	if err != nil {
		return nil, err
	}

	r := hkdf.Expand(sha256.New, key, hkdfLabelBytes)
	out := make([]byte, length)
	n, err := r.Read(out)
	if err != nil {
		return nil, err
	}
	if n != int(length) {
		return nil, errors.New("failed to read all bytes, short read")
	}
	return out, nil
}

func ComputeHeaderProtection(clientHpKey, sample []byte) ([]byte, error) {
	if len(clientHpKey) != 16 || len(sample) != 16 {
		panic("invalid input")
	}

	// AES-128-ECB
	cipher, err := aes.NewCipher([]byte(clientHpKey))
	if err != nil {
		return nil, err
	}

	var headerProtection []byte = make([]byte, 16)
	cipher.Encrypt(headerProtection, sample)

	return headerProtection[:5], nil
}

func DecryptAES128GCM(iv []byte, recordNum uint64, key, ciphertext, recdata, authtag []byte) (plaintext []byte, err error) {
	buildIV(iv, recordNum)

	if len(iv) != 12 || len(key) != 16 || len(authtag) != 16 {
		return nil, errors.New("invalid input")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}

	aesgcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	return aesgcm.Open(nil, iv, append(ciphertext, authtag...), recdata)
}

// https://quic.xargs.org/files/aes_128_gcm_decrypt.c
//
// static void build_iv(uchar *iv, uint64_t seq)
//
//	{
//		size_t i;
//		for (i = 0; i < 8; i++) {
//			iv[gcm_ivlen-1-i] ^= ((seq>>(i*8))&0xFF);
//		}
//	}
func buildIV(iv []byte, seq uint64) {
	for i := 0; i < 8; i++ {
		iv[11-i] ^= byte((seq >> (i * 8)) & 0xFF)
	}
}
