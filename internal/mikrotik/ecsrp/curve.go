package ecsrp

import (
	"crypto/sha256"
	"errors"
	"math/big"
)

// WCurve implements elliptic curve operations for ECSRP-5
// This is a port of MarginResearch's WCurve class from mikrotik_authentication
// It handles conversions between Montgomery and Weierstrass curves
type WCurve struct {
	// Curve parameters for Curve25519 variant used by MikroTik
	p         *big.Int // Prime field modulus
	r         *big.Int // Curve order
	montA     *big.Int // Montgomery curve A parameter (486662)
	convFromM *big.Int // Conversion constant from Montgomery
	conv      *big.Int // Conversion constant to Montgomery
	a         *big.Int // Weierstrass a parameter
	b         *big.Int // Weierstrass b parameter
	h         *big.Int // Cofactor (8)
	gx        *big.Int // Generator point x coordinate
	gy        *big.Int // Generator point y coordinate
}

// Point represents an elliptic curve point in affine coordinates
type Point struct {
	X *big.Int
	Y *big.Int
}

// NewWCurve creates a new WCurve instance with MikroTik's curve parameters
func NewWCurve() *WCurve {
	w := &WCurve{}

	// Initialize curve parameters (from MarginResearch elliptic_curves.py)
	w.p = hexToBigInt("7fffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffed")
	w.r = hexToBigInt("1000000000000000000000000000000014def9dea2f79cd65812631a5cf5d3ed")
	w.montA = big.NewInt(486662)
	w.h = big.NewInt(8)

	// Weierstrass curve parameters
	w.a = hexToBigInt("2aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa984914a144")
	w.b = hexToBigInt("7b425ed097b425ed097b425ed097b425ed097b425ed097b4260b5e9c7710c864")

	// Conversion constants
	// convFromM = montA * modinv(3, p) % p
	three := big.NewInt(3)
	threeInv := new(big.Int).ModInverse(three, w.p)
	w.convFromM = new(big.Int).Mul(w.montA, threeInv)
	w.convFromM.Mod(w.convFromM, w.p)

	// conv = (p - montA * modinv(3, p)) % p
	w.conv = new(big.Int).Sub(w.p, w.convFromM)
	w.conv.Mod(w.conv, w.p)

	// Generator point (base point with x=9)
	g := w.LiftX(big.NewInt(9), false)
	if g == nil {
		panic("failed to compute generator point")
	}
	w.gx = g.X
	w.gy = g.Y

	return w
}

// GenPublicKey generates a public key from a private key (ECPEPKGP-SRP-A)
// Returns the Montgomery x-coordinate and y-parity
func (w *WCurve) GenPublicKey(priv []byte) ([]byte, bool, error) {
	if len(priv) != 32 {
		return nil, false, errors.New("private key must be 32 bytes")
	}

	privInt := new(big.Int).SetBytes(priv)

	// Multiply generator by private key
	pt := w.scalarMult(&Point{X: w.gx, Y: w.gy}, privInt)

	// Convert to Montgomery coordinates
	return w.ToMontgomery(pt)
}

// ToMontgomery converts a Weierstrass point to Montgomery x-coordinate
// Returns x-coordinate as 32 bytes and y-parity
func (w *WCurve) ToMontgomery(pt *Point) ([]byte, bool, error) {
	if pt == nil {
		return nil, false, errors.New("point is nil")
	}

	// x_montgomery = (x_weierstrass + conversion) % p
	xMont := new(big.Int).Add(pt.X, w.conv)
	xMont.Mod(xMont, w.p)

	// Pad to 32 bytes
	xBytes := make([]byte, 32)
	xMont.FillBytes(xBytes)

	// Y parity (LSB of Y coordinate)
	parity := pt.Y.Bit(0) == 1

	return xBytes, parity, nil
}

// LiftX recovers a Weierstrass point from Montgomery x-coordinate
// This implements both Montgomery curve point recovery and conversion
func (w *WCurve) LiftX(xMont *big.Int, parity bool) *Point {
	// Montgomery curve equation: y^2 = x^3 + A*x^2 + x
	xMont = new(big.Int).Mod(xMont, w.p)

	// Compute y^2 on Montgomery curve
	x2 := new(big.Int).Mul(xMont, xMont)
	x2.Mod(x2, w.p)

	x3 := new(big.Int).Mul(x2, xMont)
	x3.Mod(x3, w.p)

	ax2 := new(big.Int).Mul(w.montA, x2)
	ax2.Mod(ax2, w.p)

	ySquared := new(big.Int).Add(x3, ax2)
	ySquared.Add(ySquared, xMont)
	ySquared.Mod(ySquared, w.p)

	// Convert x to Weierstrass coordinates
	xWeier := new(big.Int).Add(xMont, w.convFromM)
	xWeier.Mod(xWeier, w.p)

	// Find y coordinate (Tonelli-Shanks algorithm)
	ys := sqrtModP(ySquared, w.p)
	if len(ys) == 0 {
		return nil
	}

	// Select y based on parity
	var y *big.Int
	if ys[0].Bit(0) == 1 && parity {
		y = ys[0]
	} else if ys[1].Bit(0) == 1 && parity {
		y = ys[1]
	} else if ys[0].Bit(0) == 0 && !parity {
		y = ys[0]
	} else {
		y = ys[1]
	}

	pt := &Point{X: xWeier, Y: y}

	// Verify point is on curve
	if !w.isOnCurve(pt) {
		return nil
	}

	return pt
}

// Redp1 implements ECEDP with hash (from MarginResearch)
// Hashes x-coordinate until a valid point is found
func (w *WCurve) Redp1(x []byte, parity bool) *Point {
	// Hash the input
	h := sha256.Sum256(x)
	xHash := h[:]

	for {
		// Hash again
		h2 := sha256.Sum256(xHash)
		xInt := new(big.Int).SetBytes(h2[:])

		// Try to lift
		pt := w.LiftX(xInt, parity)
		if pt != nil {
			return pt
		}

		// Increment and retry
		xInt.SetBytes(xHash)
		xInt.Add(xInt, big.NewInt(1))
		xHash = make([]byte, 32)
		xInt.FillBytes(xHash)
	}
}

// GenPasswordValidatorPriv generates the password validator private key
// This is the 'i' value in ECSRP-5: i = SHA256(salt || SHA256(username:password))
func (w *WCurve) GenPasswordValidatorPriv(username, password string, salt []byte) []byte {
	if len(salt) != 16 {
		panic("salt must be 16 bytes")
	}

	// Inner hash: SHA256(username:password)
	credentials := username + ":" + password
	innerHash := sha256.Sum256([]byte(credentials))

	// Outer hash: SHA256(salt || innerHash)
	combined := append(salt, innerHash[:]...)
	outerHash := sha256.Sum256(combined)

	return outerHash[:]
}

// FiniteFieldValue reduces a value modulo the curve order
func (w *WCurve) FiniteFieldValue(a *big.Int) *big.Int {
	return new(big.Int).Mod(a, w.r)
}

// MultiplyByG multiplies the generator point by scalar
func (w *WCurve) MultiplyByG(scalar *big.Int) *Point {
	return w.scalarMult(&Point{X: w.gx, Y: w.gy}, scalar)
}

// Multiply multiplies a point by a scalar
func (w *WCurve) Multiply(point *Point, scalar *big.Int) *Point {
	return w.scalarMult(point, scalar)
}

// Add adds two points on the Weierstrass curve
func (w *WCurve) Add(p1, p2 *Point) *Point {
	if p1 == nil {
		return p2
	}
	if p2 == nil {
		return p1
	}

	// Point at infinity
	if p1.X.Cmp(p2.X) == 0 && p1.Y.Cmp(p2.Y) != 0 {
		return nil // Point at infinity
	}

	var lambda *big.Int

	if p1.X.Cmp(p2.X) == 0 && p1.Y.Cmp(p2.Y) == 0 {
		// Point doubling: lambda = (3*x1^2 + a) / (2*y1)
		x1Sq := new(big.Int).Mul(p1.X, p1.X)
		x1Sq.Mod(x1Sq, w.p)

		numerator := new(big.Int).Mul(x1Sq, big.NewInt(3))
		numerator.Add(numerator, w.a)
		numerator.Mod(numerator, w.p)

		denominator := new(big.Int).Mul(p1.Y, big.NewInt(2))
		denominator.Mod(denominator, w.p)

		denomInv := new(big.Int).ModInverse(denominator, w.p)
		lambda = new(big.Int).Mul(numerator, denomInv)
		lambda.Mod(lambda, w.p)
	} else {
		// Point addition: lambda = (y2 - y1) / (x2 - x1)
		numerator := new(big.Int).Sub(p2.Y, p1.Y)
		numerator.Mod(numerator, w.p)

		denominator := new(big.Int).Sub(p2.X, p1.X)
		denominator.Mod(denominator, w.p)

		denomInv := new(big.Int).ModInverse(denominator, w.p)
		lambda = new(big.Int).Mul(numerator, denomInv)
		lambda.Mod(lambda, w.p)
	}

	// x3 = lambda^2 - x1 - x2
	x3 := new(big.Int).Mul(lambda, lambda)
	x3.Sub(x3, p1.X)
	x3.Sub(x3, p2.X)
	x3.Mod(x3, w.p)

	// y3 = lambda * (x1 - x3) - y1
	y3 := new(big.Int).Sub(p1.X, x3)
	y3.Mul(y3, lambda)
	y3.Sub(y3, p1.Y)
	y3.Mod(y3, w.p)

	return &Point{X: x3, Y: y3}
}

// scalarMult performs scalar multiplication using double-and-add
func (w *WCurve) scalarMult(point *Point, scalar *big.Int) *Point {
	if scalar.Sign() == 0 {
		return nil
	}

	// Use binary method (double-and-add)
	result := (*Point)(nil)
	addend := &Point{X: new(big.Int).Set(point.X), Y: new(big.Int).Set(point.Y)}

	// Process each bit of scalar
	for i := 0; i < scalar.BitLen(); i++ {
		if scalar.Bit(i) == 1 {
			result = w.Add(result, addend)
		}
		addend = w.Add(addend, addend) // Double
	}

	return result
}

// isOnCurve verifies a point is on the Weierstrass curve
func (w *WCurve) isOnCurve(pt *Point) bool {
	if pt == nil {
		return false
	}

	// y^2 = x^3 + a*x + b
	left := new(big.Int).Mul(pt.Y, pt.Y)
	left.Mod(left, w.p)

	x3 := new(big.Int).Mul(pt.X, pt.X)
	x3.Mul(x3, pt.X)
	x3.Mod(x3, w.p)

	ax := new(big.Int).Mul(w.a, pt.X)
	ax.Mod(ax, w.p)

	right := new(big.Int).Add(x3, ax)
	right.Add(right, w.b)
	right.Mod(right, w.p)

	return left.Cmp(right) == 0
}

// sqrtModP computes square roots modulo a prime using Tonelli-Shanks
// Returns both positive and negative roots
func sqrtModP(a, p *big.Int) []*big.Int {
	a = new(big.Int).Mod(a, p)

	if a.Sign() == 0 {
		return []*big.Int{big.NewInt(0)}
	}

	if p.Cmp(big.NewInt(2)) == 0 {
		return []*big.Int{new(big.Int).Set(a)}
	}

	// Legendre symbol check
	ls := legendreSymbol(a, p)
	if ls != 1 {
		return nil
	}

	// Special case: p ≡ 3 (mod 4)
	pMod4 := new(big.Int).Mod(p, big.NewInt(4))
	if pMod4.Cmp(big.NewInt(3)) == 0 {
		exp := new(big.Int).Add(p, big.NewInt(1))
		exp.Div(exp, big.NewInt(4))
		x := new(big.Int).Exp(a, exp, p)
		negX := new(big.Int).Sub(p, x)
		return []*big.Int{x, negX}
	}

	// General Tonelli-Shanks algorithm
	q := new(big.Int).Sub(p, big.NewInt(1))
	s := 0
	for new(big.Int).Mod(q, big.NewInt(2)).Sign() == 0 {
		s++
		q.Div(q, big.NewInt(2))
	}

	// Find a quadratic non-residue
	z := big.NewInt(1)
	for legendreSymbol(z, p) != -1 {
		z.Add(z, big.NewInt(1))
	}

	c := new(big.Int).Exp(z, q, p)

	qPlus1 := new(big.Int).Add(q, big.NewInt(1))
	qPlus1.Div(qPlus1, big.NewInt(2))
	x := new(big.Int).Exp(a, qPlus1, p)

	t := new(big.Int).Exp(a, q, p)
	m := s

	for t.Cmp(big.NewInt(1)) != 0 {
		// Find least i such that t^(2^i) = 1
		i := 1
		temp := new(big.Int).Mul(t, t)
		temp.Mod(temp, p)

		for temp.Cmp(big.NewInt(1)) != 0 && i < m {
			temp.Mul(temp, temp)
			temp.Mod(temp, p)
			i++
		}

		// b = c^(2^(m-i-1))
		exp := m - i - 1
		b := new(big.Int).Set(c)
		for j := 0; j < exp; j++ {
			b.Mul(b, b)
			b.Mod(b, p)
		}

		x.Mul(x, b)
		x.Mod(x, p)

		c.Mul(b, b)
		c.Mod(c, p)

		t.Mul(t, c)
		t.Mod(t, p)

		m = i
	}

	negX := new(big.Int).Sub(p, x)
	return []*big.Int{x, negX}
}

// legendreSymbol computes the Legendre symbol (a/p)
func legendreSymbol(a, p *big.Int) int {
	exp := new(big.Int).Sub(p, big.NewInt(1))
	exp.Div(exp, big.NewInt(2))
	l := new(big.Int).Exp(a, exp, p)

	pMinus1 := new(big.Int).Sub(p, big.NewInt(1))
	if l.Cmp(pMinus1) == 0 {
		return -1
	}
	return int(l.Int64())
}

// hexToBigInt converts hex string to big.Int
func hexToBigInt(s string) *big.Int {
	n := new(big.Int)
	n.SetString(s, 16)
	return n
}
