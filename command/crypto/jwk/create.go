package jwk

import (
	"bytes"
	gocrypto "crypto"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"

	"github.com/pkg/errors"
	"github.com/smallstep/cli/crypto/pem"
	"github.com/smallstep/cli/crypto/randutil"
	"github.com/smallstep/cli/errs"
	"github.com/smallstep/cli/jose"
	"github.com/smallstep/cli/utils"
	"github.com/urfave/cli"
)

const (
	// 128-bit salt
	pbkdf2SaltSize = 16
	// 100k iterations. Nist recommends at least 10k, 1Passsword uses 100k.
	pbkdf2Iterations = 100000
)

func createCommand() cli.Command {
	return cli.Command{
		Name:   "create",
		Action: cli.ActionFunc(createAction),
		Usage:  "create a JWK (JSON Web Key)",
		UsageText: `**step crypto jwk create** <public-jwk-file> <private-jwk-file>
    [**--kty**=<type>] [**--alg**=<algorithm>] [**--use**=<use>]
    [**--size**=<size>] [**--crv**=<curve>] [**--kid**=<kid>]
    [**--from-pem**=<pem-file>]`,
		Description: `**step crypto jwk create** generates a new JWK (JSON Web Key) or constructs a
JWK from an existing key. The generated JWK conforms to RFC7517 and can be used
to sign and encrypt data using JWT, JWS, and JWE.

Files containing private keys are encrypted by default. You'll be prompted for
a password. Keys are written with file mode **0600** (i.e., readable and
writable only by the current user).

All flags are optional. Defaults are suitable for most use cases.

## POSITIONAL ARGUMENTS

<public-jwk-file>
:  Path to which the the public JWK should be written

<private-jwk-file>
:  Path to which the (JWE encrypted) private JWK should be written

## EXIT CODES

This command returns 0 on success and \>0 if any error occurs.

## SECURITY CONSIDERATIONS

All security considerations from **step help crypto** are relevant here.

**Preventing hostile disclosure of non-public key material**

: It is critical that any private and symmetric key material be protected from
  unauthorized disclosure or modification. This includes the private key for
  asymmetric key types (RSA, EC, and OKP) and the shared secret for symmetric key
  types (oct). One means of protection is encryption. Keys can also be stored in
  hardware or software "security enclaves" such as HSMs and TPMs or operating
  system keychain management tools.

**Key provenance and bindings**

: Key provenance should always be scrutinized. You should not trust a key that
  was obtained in an untrustworthy manner (e.g., non-TLS HTTP).

: Usually applications use keys to make authorization decisions based on
  attributes "bound" to the key such as the key owner's name or role. In these
  scenarios the strength of the system's security depends on the strength of
  these "bindings". There are a variety of mechanisms for securely binding
  attributes to keys, including:

  * Cryptographically binding attributes to the public key using x509
    certificates (e.g., as defined in PKIX / RFC2580)
  * Cryptographically binding attributes to the public key using JWTs
  * Storing the public key or (hashed) shared secret along with the bound
    attributes in a secure database

: Cryptographic mechanisms require establishing a "root of trust" that can sign
  the bindings (the certificates or JWTs) asserting that the bound attributes are
  correct.

## STANDARDS

[RFC7517]
: Jones, M., "JSON Web Key (JWK)", https://tools.ietf.org/html/rfc7517

[RFC7518]
: Jones, M., "JSON Web Algorithms (JWA)", https://tools.ietf.org/html/rfc7518

[RFC7638]
: M. Jones, N. Sakimura., "JSON Web Key (JWK) Thumbprint",
  https://tools.ietf.org/html/rfc7638

[RFC8037]
: I. Liusvaara., "CFRG Elliptic Curve Diffie-Hellman (ECDH) and Signatures in
  JSON Object Signing and Encryption (JOSE)",
  https://tools.ietf.org/html/rfc8037

## EXAMPLES

Create a new JWK using default options:

'''
$ step crypto jwk create jwk.pub.json jwk.json
'''

Create an RSA JWK:

'''
$ step crypto jwk create rsa.pub.json rsa.json --kty RSA
'''

Create a symmetric key (oct key type):

'''
$ step crypto jwk create oct.pub.json oct.json --kty oct
'''

Create a key for use with the Ed25519 cryptosystem:

'''
$ step crypto jwk create ed.pub.json ed.json \
    --kty OKP --crv Ed25519
'''

Create a key from an existing PEM file:

'''
$ step crypto jwk create jwk.pub.json jwk.json 
    --from-pem key.pem
'''

Create an 4096 bit RSA encryption key:

'''
$ step crypto jwk create rsa-enc.pub.json rsa-enc.json \
   --kty RSA --size 4096 --use enc
'''

Create a 192 bit symmetric encryption key for use with AES Key Wrap:

'''
$ step crypto jwk create kw.pub.json kw.json \
    --kty oct --size 192 --use enc --alg A192GCMKW
'''
`,
		Flags: []cli.Flag{
			cli.StringFlag{
				Name:  "kty, type",
				Value: "EC",
				Usage: `The <type> of key to create. Corresponds to the **"kty"** JWK parameter.
If unset, default is EC.

: <type> is a case-sensitive string and must be one of:

    **EC**
    :  Create an **elliptic curve** keypair

    **oct**
    :  Create a **symmetric key** (octet stream)

    **OKP**
    :  Create an octet key pair (for **"Ed25519"** curve)

    **RSA**
    :  Create an **RSA** keypair
`,
			},
			cli.IntFlag{
				Name: "size",
				Usage: `The <size> (in bits) of the key for RSA and oct key types. RSA keys require a
minimum key size of 2048 bits. If unset, default is 2048 bits for RSA keys and 128 bits for oct keys.`,
			},
			cli.StringFlag{
				Name: "crv, curve",
				Usage: `The elliptic <curve> to use for EC and OKP key types. Corresponds
to the **"crv"** JWK parameter. Valid curves are defined in JWA [RFC7518]. If
unset, default is P-256 for EC keys and Ed25519 for OKP keys.

: <curve> is a case-sensitive string and must be one of:

    **P-256**
    :  NIST P-256 Curve

    **P-384**
    :  NIST P-384 Curve

    **P-521**
    :  NIST P-521 Curve

    **Ed25519**
    :  Ed25519 Curve
`,
			},
			cli.StringFlag{
				Name: "alg, algorithm",
				Usage: `The <algorithm> intended for use with this key. Corresponds to the
**"alg"** JWK parameter. <algorithm> is case-sensitive. If unset, the default
depends on the key use, key type, and curve (for EC and OKP keys). Defaults are:

: | key type | use | curve   | default algorithm |
  |----------|-----|---------|-------------------|
  | EC       | sig | P-256   | ES256             |
  | EC       | sig | P-384   | ES384             |
  | EC       | sig | P-521   | ES512             |
  | oct      | sig | N/A     | HS256             |
  | RSA      | sig | N/A     | RS256             |
  | OKP      | sig | Ed25519 | EdDSA             |
  | EC       | enc | P-256   | ECDH-ES           |
  | EC       | enc | P-384   | ECDH-ES           |
  | EC       | enc | P-521   | ECDH-ES           |
  | oct      | enc | N/A     | A256GCMKW         |
  | RSA      | enc | N/A     | RSA-OAP-256       |

: If the key **"use"** is **"sig"** (signing) <algorithm> must be one of:

    **HS256**
    :  HMAC using SHA-256

    **HS384**
    :  HMAC using SHA-384

    **HS512**
	:  HMAC using SHA-512

    **RS256**
    :  RSASSA-PKCS1-v1_5 using SHA-256

    **RS384**
    :  RSASSA-PKCS1-v1_5 using SHA-384

    **RS512**
    :  RSASSA-PKCS1-v1_5 using SHA-512

    **ES256**
    :  ECDSA using P-256 and SHA-256

    **ES384**
    :  ECDSA using P-384 and SHA-384

    **ES512**
    :  ECDSA using P-521 and SHA-512

    **PS256**
    :  RSASSA-PSS using SHA-256 and MGF1 with SHA-256

    **PS384**
    :  RSASSA-PSS using SHA-384 and MGF1 with SHA-384

    **PS512**
    :  RSASSA-PSS using SHA-512 and MGF1 with SHA-512

    **EdDSA**
    :  EdDSA signature algorithm

: If the key **"use"** is **"enc"** (encryption) <algorithm> must be one of:

    **RSA1_5**
    :  RSAES-PKCS1-v1_5

    **RSA-OAEP**
    :  RSAES OAEP using default parameters

    **RSA-OAEP-256**
    :  RSAES OAEP using SHA-256 and MGF1 with SHA-256

    **A128KW**
    :  AES Key Wrap with default initial value using 128-bit key

    **A192KW**
    :  AES Key Wrap with default initial value using 192-bit key

    **A256KW**
    :  AES Key Wrap with default initial value using 256-bit key

    **dir**
    :  Direct use of a shared symmetric key as the content encryption key (CEK)

    **ECDH-ES**
    :  Elliptic Curve Diffie-Hellman Ephemeral Static key agreement

    **ECDH-ES+A128KW**
    :  ECDH-ES using Concat KDF and CEK wrapped with "A128KW"

    **ECDH-ES+A192KW**
    :  ECDH-ES using Concat KDF and CEK wrapped with "A192KW"

    **ECDH-ES+A256KW**
    :  ECDH-ES using Concat KDF and CEK wrapped with "A256KW"

    **A128GCMKW**
    :  Key wrapping with AES GCM using 128-bit key

    **A192GCMKW**
    :  Key wrapping with AES GCM using 192-bit key

    **A256GCMKW**
    :  Key wrapping with AES GCM using 256-bit key

    **PBES2-HS256+A128KW**
    :  PBES2 with HMAC SHA-256 and "A128KW" wrapping

    **PBES2-HS384+A192KW**
    :  PBES2 with HMAC SHA-256 and "A192KW" wrapping

    **PBES2-HS512+A256KW**
    :  PBES2 with HMAC SHA-256 and "A256KW" wrapping`,
			},
			cli.StringFlag{
				Name:  "use",
				Value: "sig",
				Usage: `The intended <use> of the public key. Corresponds to the "use" JWK parameter.
The "use" parameter indicates whether the public key is used for encrypting
data or verifying the signature on data.

: <use> is a case-sensitive string and may be one of:

    **sig**
	:  The public key is used for verifying signatures.

    **enc**
	:  The public key is used for encrypting data.

: Other values may be used but the generated JWKs will not work for signing or
encryption with this tool.`,
			},
			cli.StringFlag{
				Name: "kid",
				Usage: `The <kid> (key ID) for this JWK. Corresponds to the
"kid" JWK parameter. Used to identify an individual key in a JWK Set, for
example. <kid> is a case-sensitive string. If unset, the JWK Thumbprint
[RFC7638] is used as <kid>. See **step help crypto jwk thumbprint** for more
information on JWK Thumbprints.`,
			},
			cli.StringFlag{
				Name:   "key-ops",
				Hidden: true, // Not currently implemented
				Usage: `The operation(s) for which the key is intended to be used. Corresponds to
the "key_ops" JWK parameter. The '--key-ops' flag can be used multiple times
to indicate multiple intended operations.

  <key-op> can be one of the values defined in RFC7517:
    sign
      Compute digital signature or MAC
    verify
      Verify digital signature or MAC
    encrypt
      Encrypt content
    decrypt
      Decrypt content and validate decryption, if applicable
    wrapKey
      Encrypt key
    unwrapKey
      Decrypt key and validate decryption, if applicable
    deriveKey
      Derive key
    deriveBits
      Derive bits not to be used as a key

  The key operation values are case-sensitive strings. Other values may be
used, but values must not be duplicated.

  The '--use' and '--key-ops' flags cannot be used together without also
passing the '--subtle' flag. The '--subtle' flag allows both flags to be used
in a consistent way (e.g., '--key-ops=encrypt --key-ops=decrypt --use=enc').
Multiple unrelated operations (e.g., '--key-ops=encrypt --key-ops=sign') or
inconsistent combinations of '--use' and '--key-ops' (e.g., '--use=enc
--key-ops=sign') are not allowed without also passing the '--insecure' flag
because of potential vulnerabilities associated with using the same key with
multiple algorithms.

  Related operations include:
    sign + verify
    encrypt + decrypt
    wrapKey + unwrapKey
  If multiple values are passed and at least one is a non-standard value the
'--subtle' flag is required as you must verify that the operations are
related.`,
			},
			cli.StringSliceFlag{
				Name:   "from-certificate",
				Usage:  `TODO: usage is missing.`,
				Hidden: true,
			},
			cli.StringFlag{
				Name: "from-pem",
				Usage: `Create a JWK representing the key encoded in an
existing <pem-file> instead of creating a new key.`,
			},
			cli.BoolFlag{
				Name: "no-password",
				Usage: `Do not ask for a password to encrypt the JWK. Sensitive
key material will be written to disk unencrypted. This is not
recommended. Requires **--insecure** flag.`,
			},
			cli.BoolFlag{
				Name:   "subtle",
				Hidden: true,
			},
			cli.BoolFlag{
				Name:   "insecure",
				Hidden: true,
			},
		},
	}
}

func createAction(ctx *cli.Context) error {
	// require public and private files
	if err := errs.NumberOfArguments(ctx, 2); err != nil {
		return err
	}

	// Use password to protect private JWK by default
	usePassword := true
	if ctx.Bool("no-password") {
		if ctx.Bool("insecure") {
			usePassword = false
		} else {
			return errs.RequiredInsecureFlag(ctx, "no-password")
		}
	}

	pubFile := ctx.Args().Get(0)
	privFile := ctx.Args().Get(1)
	if pubFile == privFile {
		return errs.EqualArguments(ctx, "public-jwk-file", "private-jwk-file")
	}

	kty := ctx.String("kty")
	crv := ctx.String("crv")
	alg := ctx.String("alg")
	use := ctx.String("use")
	kid := ctx.String("kid")
	size := ctx.Int("size")
	pemFile := ctx.String("from-pem")

	switch kty {
	case "EC":
		if ctx.IsSet("size") {
			return errs.IncompatibleFlag(ctx, "size", "--kty EC")
		}
	case "RSA":
		if ctx.IsSet("crv") {
			return errs.IncompatibleFlag(ctx, "crv", "--kty RSA")
		}
		// If size is not set it will use a safe default
		if ctx.IsSet("size") {
			if size < 2048 && !ctx.Bool("insecure") {
				return errs.MinSizeInsecureFlag(ctx, "size", "2048")
			}
			if size <= 0 {
				return errs.MinSizeFlag(ctx, "size", "0")
			}
		}
	case "OKP":
		if ctx.IsSet("size") {
			return errs.IncompatibleFlag(ctx, "size", "--kty OKP")
		}
	case "oct":
		if ctx.IsSet("crv") {
			return errs.IncompatibleFlag(ctx, "crv", "--kty oct")
		}
		// If size is not set it will use a safe default
		if ctx.IsSet("size") {
			if size < 16 && !ctx.Bool("insecure") {
				return errs.MinSizeInsecureFlag(ctx, "size", "16")
			}
			if size <= 0 {
				return errs.MinSizeFlag(ctx, "size", "0")
			}
		}
	default:
		return errs.InvalidFlagValue(ctx, "kty", kty, "EC, RSA, OKP, or oct")
	}

	// Generate or read secrets
	var err error
	var jwk *jose.JSONWebKey
	switch {
	case pemFile != "":
		jwk, err = jose.GenerateJWKFromPEM(pemFile)
	default:
		jwk, err = jose.GenerateJWK(kty, crv, alg, use, kid, size)
	}

	if err != nil {
		return err
	}

	if ctx.IsSet("kid") {
		jwk.KeyID = ctx.String("kid")
	} else {
		// A hash of a symmetric key can leak information, so we only thumbprint asymmetric keys.
		if kty != "oct" {
			hash, err := jwk.Thumbprint(gocrypto.SHA256)
			if err != nil {
				return errors.Wrap(err, "error generating JWK thumbprint")
			}
			jwk.KeyID = base64.RawURLEncoding.EncodeToString(hash)
		}
	}
	jwk.Use = use

	if jwk.Algorithm == "" {
		jwk.Algorithm = alg
	}

	if err := jose.ValidateJWK(jwk); err != nil {
		return err
	}

	// Add x5c (X.509 Certificate Chain) parameter
	crtFiles := ctx.StringSlice("from-certificate")
	for _, name := range crtFiles {
		crt, err := pem.ReadCertificate(name)
		if err != nil {
			return err
		}
		jwk.Certificates = append(jwk.Certificates, crt)
	}

	var jwkPub jose.JSONWebKey
	if jose.IsSymmetric(jwk) {
		jwkPub = *jwk
	} else {
		jwkPub = jwk.Public()
	}

	// Create and write public JWK
	b, err := json.MarshalIndent(jwkPub, "", "  ")
	if err != nil {
		return errors.Wrap(err, "error marshaling JWK")
	}
	if err := utils.WriteFile(pubFile, b, 0600); err != nil {
		return errs.FileError(err, pubFile)
	}

	if jwk.IsPublic() {
		fmt.Fprintln(os.Stderr, "Only the public JWK was generated.")
		fmt.Fprintln(os.Stderr, "Cannot retrieve a private key from a public one.")
		return nil
	}

	// Create and write private JWK
	if usePassword {
		var rcpt jose.Recipient
		// Generate JWE encryption key.
		if jose.SupportsPBKDF2 {
			key, err := utils.ReadPassword("Please enter the password to encrypt the private JWK: ")
			if err != nil {
				return errors.Wrap(err, "error reading password")
			}

			salt, err := randutil.GetRandomSalt(pbkdf2SaltSize)
			if err != nil {
				return err
			}

			rcpt = jose.Recipient{
				Algorithm: jose.PBES2_HS256_A128KW,
				Key:       []byte(key),
				P2C:       pbkdf2Iterations,
				P2S:       salt,
			}
		} else {
			key, err := randutil.RandAlphanumeric(32)
			if err != nil {
				return errors.Wrap(err, "error generating password")
			}
			fmt.Printf("Private JWK file '%s' will be encrypted with the key:\n%s\n", privFile, key)
			rcpt = jose.Recipient{Algorithm: jose.A128KW, Key: []byte(key)}
		}

		b, err = json.Marshal(jwk)
		if err != nil {
			return errors.Wrap(err, "error marshaling JWK")
		}

		encrypter, err := jose.NewEncrypter(jose.A128GCM, rcpt, nil)
		if err != nil {
			return errors.Wrap(err, "error creating cipher")
		}

		obj, err := encrypter.Encrypt(b)
		if err != nil {
			return errors.Wrap(err, "error encrypting JWK")
		}

		var out bytes.Buffer
		if err := json.Indent(&out, []byte(obj.FullSerialize()), "", "  "); err != nil {
			return errors.Wrap(err, "error formatting JSON")
		}
		b = out.Bytes()
	} else {
		b, err = json.MarshalIndent(jwk, "", "  ")
		if err != nil {
			return errors.Wrap(err, "error marshaling JWK")
		}
	}
	if err := utils.WriteFile(privFile, b, 0600); err != nil {
		return errs.FileError(err, privFile)
	}

	return nil
}