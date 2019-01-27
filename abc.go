package zksigma

import (
	"crypto/rand"
	"math/big"
)

// =================== a * b = c MULTIPLICATIVE RELATIONSHIP ===================
// The following is to generate a proof if the transaction we are checking
// involves the bank being audited

// ABCProof is a proof that generates a proof that the relationship between three
// scalars a,b and c is ab = c
//
//  Public: G, H, CM, B, C, CMTok where
//  - CM = vG + uaH // we do not know ua, only v
//  - B = inv(v)G + ubH //inv is multiplicative inverse, in the case of v = 0, inv(v) = 0
//  - C = (v * inv(v))G + ucH
//  - CMTok = rPK = r(skH) // same r from A
//
//  Prover									Verifier
//  ======                                  ======
//  generate in order:
//  - commitment of inv(v), B
//  - commitment of v * inv(v), C // either 0 or 1 ONLY
//  - Disjunctive proof of a = 0 or c = 1
//  select u1, u2, u3 at random
//  select ub, uc at random
//  Compute:
//  - T1 = u1G + u2CMTok
//  - T2 = u1B + u3H
//  - c = HASH(G,H,CM,CMTok,B,C,T1,T2)
//  Compute:
//  - j = u1 + v * c
//  - k = u2 + inv(sk) * c
//  - l = u3 + (uc - v * ub) * c
//
//  disjuncAC, B, C, T1, T2, c, j, k, l ------->
//         									disjuncAC ?= true
//         									c ?= HASH(G,H,CM,CMTok,B,C,T1,T2)
//         									cCM + T1 ?= jG + kCMTok
//         									cC + T2 ?= jB + lH
type ABCProof struct {
	B         ECPoint  // commitment for b = 0 OR inv(v)
	C         ECPoint  // commitment for c = 0 OR 1 ONLY
	T1        ECPoint  // T1 = u1G + u2MTok
	T2        ECPoint  // T2 = u1B + u3H
	Challenge *big.Int //c = HASH(G,H,CM,CMTok,B,C,T1,T2)
	j         *big.Int // j = u1 + v * c
	k         *big.Int // k = u2 + inv(sk) * c
	l         *big.Int // l = u3 + (uc - v * ub) * c
	CToken    ECPoint
	disjuncAC *DisjunctiveProof
}

// NewABCProof generates a proof that the relationship between three scalars a,b and c is ab = c,
// in commitments A, B and C respectively.
// Option Left is proving that A and C commit to zero and simulates that A, B and C commit to v, inv(v) and 1 respectively.
// Option Right is proving that A, B and C commit to v, inv(v) and 1 respectively and sumulating that A and C commit to 0.
func NewABCProof(CM, CMTok ECPoint, value, sk *big.Int, option Side) (*ABCProof, error) {

	// We cannot check that CM log is acutally the value, but the verification should catch that

	u1, err := rand.Int(rand.Reader, ZKCurve.C.Params().N)
	if err != nil {
		return nil, err
	}
	u2, err := rand.Int(rand.Reader, ZKCurve.C.Params().N)
	if err != nil {
		return nil, err
	}

	u3, err := rand.Int(rand.Reader, ZKCurve.C.Params().N)
	if err != nil {
		return nil, err
	}

	ub, err := rand.Int(rand.Reader, ZKCurve.C.Params().N)
	if err != nil {
		return nil, err
	}
	uc, err := rand.Int(rand.Reader, ZKCurve.C.Params().N)
	if err != nil {
		return nil, err
	}

	B := ECPoint{}
	C := ECPoint{}
	CToken := ZKCurve.H.Mult(sk).Mult(uc)

	var disjuncAC *DisjunctiveProof
	var e error
	// Disjunctive Proof of a = 0 or c = 1
	if option == Left && value.Cmp(big.NewInt(0)) == 0 {
		// MUST:a = 0! ; side = left
		// B = 0 + ubH, here since we want to prove v = 0, we later accomidate for the lack of inverses
		B = PedCommitR(new(big.Int).ModInverse(big.NewInt(0), ZKCurve.C.Params().N), ub)

		// C = 0 + ucH
		C = PedCommitR(big.NewInt(0), uc)

		// CM is considered the "base" of CMTok since it would be only uaH and not ua sk H
		// C - G is done regardless of the c = 0 or 1 becuase in the case c = 0 it does matter what that random number is
		disjuncAC, e = NewDisjunctiveProof(CM, CMTok, ZKCurve.H, C.Sub(ZKCurve.G), sk, Left)
	} else if option == Right && value.Cmp(big.NewInt(0)) != 0 {
		// MUST:c = 1! ; side = right

		B = PedCommitR(new(big.Int).ModInverse(value, ZKCurve.C.Params().N), ub)

		// C = G + ucH
		C = PedCommitR(big.NewInt(1), uc)

		// Look at notes a couple lines above on what the input is like this
		disjuncAC, e = NewDisjunctiveProof(CM, CMTok, ZKCurve.H, C.Sub(ZKCurve.G), uc, Right)
	} else {
		return &ABCProof{}, &errorProof{"ABCProof", "invalid side-value pair passed"}
	}

	if e != nil {
		return &ABCProof{}, &errorProof{"ABCProof", "DisjuntiveProve within ABCProve failed to generate"}
	}

	// CMTok is Ta for the rest of the proof
	// T1 = u1G + u2Ta
	// u1G
	u1G := ZKCurve.G.Mult(u1)
	// u2Ta
	u2Ta := CMTok.Mult(u2)
	// Sum the above two
	T1 := u1G.Add(u2Ta)

	// T2 = u1B + u3H
	// u1B
	u1B := B.Mult(u1)
	// u3H
	u3H := ZKCurve.H.Mult(u3)
	// Sum of the above two
	T2 := u1B.Add(u3H)

	// c = HASH(G,H,CM,CMTok,B,C,T1,T2)
	Challenge := GenerateChallenge(ZKCurve.G.Bytes(), ZKCurve.H.Bytes(),
		CM.Bytes(), CMTok.Bytes(),
		B.Bytes(), C.Bytes(),
		T1.Bytes(), T2.Bytes())

	// j = u1 + v * c , can be though of as s1
	j := new(big.Int).Add(u1, new(big.Int).Mul(value, Challenge))
	j = new(big.Int).Mod(j, ZKCurve.C.Params().N)

	// k = u2 + inv(sk) * c
	// inv(sk)
	isk := new(big.Int).ModInverse(sk, ZKCurve.C.Params().N)
	k := new(big.Int).Add(u2, new(big.Int).Mul(isk, Challenge))
	k = new(big.Int).Mod(k, ZKCurve.C.Params().N)

	// l = u3 + (uc - v * ub) * c
	temp1 := new(big.Int).Sub(uc, new(big.Int).Mul(value, ub))
	l := new(big.Int).Add(u3, new(big.Int).Mul(temp1, Challenge))

	return &ABCProof{
		B,
		C,
		T1,
		T2,
		Challenge,
		j, k, l, CToken,
		disjuncAC}, nil

}

/*
	proofA ?= true
	proofC ?= true
	c ?= HASH(G,H,CM,CMTok,B,C,T1,T2)
	cCM + T1 ?= jG + kCMTok
	cC + T2 ?= jB + lH
*/

// Verify checks if ABCProof aProof with appropriate commits CM and CMTok is correct
func (aProof *ABCProof) Verify(CM, CMTok ECPoint) (bool, error) {

	// Notes in ABCProof talk about why the Disjunc takes in this specific input even though it looks non-intuative
	// Here it is important that you subtract exactly 1 G from the aProof.C becuase that only allows for you to prove c = 1!
	_, status := aProof.disjuncAC.Verify(CM, CMTok, ZKCurve.H, aProof.C.Sub(ZKCurve.G))
	if status != nil {
		return false, &errorProof{"ABCVerify", "ABCProof for disjuncAC is false or not generated properly"}
	}

	Challenge := GenerateChallenge(ZKCurve.G.Bytes(), ZKCurve.H.Bytes(),
		CM.Bytes(), CMTok.Bytes(),
		aProof.B.Bytes(), aProof.C.Bytes(),
		aProof.T1.Bytes(), aProof.T2.Bytes())

	// c = HASH(G,H,CM,CMTok,B,C,T1,T2)
	if Challenge.Cmp(aProof.Challenge) != 0 {
		return false, &errorProof{"ABCVerify", "proof contains incorrect challenge"}
	}

	// cCM + T1 ?= jG + kCMTok
	// cCM
	chalA := CM.Mult(Challenge)
	// + T1
	lhs1 := chalA.Add(aProof.T1)
	//jG
	jG := ZKCurve.G.Mult(aProof.j)
	// kCMTok
	kCMTok := CMTok.Mult(aProof.k)
	// jG + kCMTok
	rhs1 := jG.Add(kCMTok)

	if !lhs1.Equal(rhs1) {
		return false, &errorProof{"ABCProof", "cCM + T1 != jG + kCMTok"}
	}

	// cC + T2 ?= jB + lH
	chalC := aProof.C.Mult(Challenge)
	lhs2 := chalC.Add(aProof.T2)

	jB := aProof.B.Mult(aProof.j)
	lH := ZKCurve.H.Mult(aProof.l)
	rhs2 := jB.Add(lH)

	if !lhs2.Equal(rhs2) {
		return false, &errorProof{"ABCVerify", "cC + T2 != jB + lH"}
	}

	return true, nil
}