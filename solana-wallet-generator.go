package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/coinbase/cb-mpc/demos-go/cb-mpc-go/api/curve"
	"github.com/coinbase/cb-mpc/demos-go/cb-mpc-go/api/mpc"
	"github.com/coinbase/cb-mpc/demos-go/cb-mpc-go/api/transport/mocknet"
	"github.com/gagliardetto/solana-go"
	"golang.org/x/sync/errgroup"
)

const (
	// Solana derivation path: m/44'/501'/0'/0'
	SolanaPath = "m/44'/501'/0'/0'"
	
	// PIN-based key derivation parameters
	PINSalt = "solana-cb-mpc-salt-2024"
	PBKDFIterations = 100000
	
	// Hardcoded PIN for testing
	TestPIN = "123456"
)

type WalletShares struct {
	SolanaAddress string `json:"solana_address"`
	S1_Server     string `json:"s1_server"`     // Server share (base64)
	S2_KMS        string `json:"s2_kms"`        // KMS share (for future recovery)
	S3_PinDerived string `json:"s3_pin_derived"` // PIN-derived share info
	MasterSeed    string `json:"master_seed"`   // HD master seed (hex)
	PublicKey     string `json:"public_key"`    // Ed25519 public key (hex)
}

func main() {
	fmt.Println("🚀 Solana Threshold Wallet Generator (2-of-3 MPC)")
	fmt.Println("=================================================")

	// Step 1: Define parties and threshold
	const nParties = 3
	const threshold = 2
	partyNames := []string{"server", "kms", "pin"}

	// Step 2: Setup EdDSA curve
	fmt.Println("\n📍 Step 1: Setting up EdDSA MPC...")
	ed25519Curve, err := curve.NewEd25519()
	if err != nil {
		log.Fatal("Failed to create Ed25519 curve:", err)
	}
	defer ed25519Curve.Free()

	// Step 3: Simulate network for DKG
	messengers := mocknet.NewMockNetwork(nParties)

	// Create access structure: 2-of-3 threshold
	accessStructure := createThresholdAccessStructure(partyNames, threshold, ed25519Curve)

	// Step 4: Perform 2-of-3 Threshold Key Generation
	fmt.Println("\n📍 Step 2: Performing 2-of-3 Threshold Key Generation...")
	var eg errgroup.Group
	keyShares := make([]mpc.EDDSAMPCKey, nParties)
	for i := 0; i < nParties; i++ {
		partyIdx := i
		eg.Go(func() error {
			job, err := mpc.NewJobMP(messengers[partyIdx], nParties, partyIdx, partyNames)
			if err != nil {
				return fmt.Errorf("party %d job creation failed: %w", partyIdx, err)
			}
			defer job.Free()

			req := &mpc.EDDSAMPCThresholdDKGRequest{
				Curve:           ed25519Curve,
				AccessStructure: accessStructure,
			}

			resp, err := mpc.EDDSAMPCThresholdDKG(job, req)
			if err != nil {
				return fmt.Errorf("party %d DKG failed: %w", partyIdx, err)
			}
			keyShares[partyIdx] = resp.KeyShare
			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		log.Fatalf("DKG protocol failed: %v", err)
	}
	fmt.Println("✅ Generated 3 threshold key shares (2-of-3)")

	// Step 5: Extract public key and derive Solana address
	fmt.Println("\n📍 Step 3: Extracting Public Key and Solana Address...")
	publicKeyPoint, err := keyShares[0].Q() // All parties have same public key
	if err != nil {
		log.Fatal("Failed to extract public key:", err)
	}
	defer publicKeyPoint.Free()

	// For Ed25519, the public key is the Y-coordinate plus a sign bit.
	// However, Solana uses the X-coordinate directly as the public key.
	publicKeyBytes := publicKeyPoint.GetX()
	if len(publicKeyBytes) != ed25519.PublicKeySize {
		log.Fatalf("Invalid Ed25519 public key length: got %d, expected %d", len(publicKeyBytes), ed25519.PublicKeySize)
	}
	solanaAddress := solana.PublicKey(publicKeyBytes)
	fmt.Printf("✅ Solana Address: %s\n", solanaAddress.String())
	fmt.Printf("✅ Public Key: %s\n", hex.EncodeToString(publicKeyBytes))

	// Step 6: Serialize and encode key shares
	fmt.Println("\n📍 Step 4: Serializing and Encoding Key Shares...")
	s1Data, err := keyShares[0].MarshalBinary()
	if err != nil {
		log.Fatal("Failed to marshal S1 share:", err)
	}
	s2Data, err := keyShares[1].MarshalBinary()
	if err != nil {
		log.Fatal("Failed to marshal S2 share:", err)
	}
	s3Data, err := keyShares[2].MarshalBinary()
	if err != nil {
		log.Fatal("Failed to marshal S3 share:", err)
	}

	s1Base64 := base64.StdEncoding.EncodeToString(s1Data)
	s2Base64 := base64.StdEncoding.EncodeToString(s2Data)
	s3Base64 := base64.StdEncoding.EncodeToString(s3Data)

	fmt.Println("\n🎉 WALLET GENERATED SUCCESSFULLY!")
	fmt.Println("=================================")
	fmt.Printf("Solana Address: %s\n", solanaAddress.String())
	fmt.Printf("Public Key:     %s\n", hex.EncodeToString(publicKeyBytes))
	fmt.Println("\n🔐 KEY SHARES (2-of-3 Threshold):")
	fmt.Println("=================================")
	fmt.Printf("S1 (Server Share): %s\n", s1Base64)
	fmt.Printf("S2 (KMS Share):    %s\n", s2Base64)
	fmt.Printf("S3 (PIN Share):    %s\n", s3Base64)

	fmt.Printf("\n📊 Share Sizes:\n")
	fmt.Printf("S1: %d bytes | S2: %d bytes | S3: %d bytes\n",
		len(s1Data), len(s2Data), len(s3Data))

	fmt.Printf("\n💡 Next Steps:\n")
	fmt.Printf("1. Store S1 on your server, S2 in a KMS, and use S3 with the user's PIN.\n")
	fmt.Printf("2. Copy these shares into the 'solana-complete-demo.go' script to test a transaction.\n")
}

// createThresholdAccessStructure creates a 2-of-3 threshold access structure
func createThresholdAccessStructure(partyNames []string, threshold int, cv curve.Curve) *mpc.AccessStructure {
	// Create leaf nodes for each party
	leaves := make([]*mpc.AccessNode, len(partyNames))
	for i, name := range partyNames {
		leaves[i] = mpc.Leaf(name)
	}

	// Create threshold root (2-of-3)
	root := mpc.Threshold("", threshold, leaves...)

	return &mpc.AccessStructure{
		Root:  root,
		Curve: cv,
	}
} 