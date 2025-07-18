use clap::{Parser, Subcommand};
use rand_core::OsRng;
use serde::{Deserialize, Serialize};
use std::fs;
use std::path::PathBuf;

use frost_ed25519 as frost;
use frost::keys::{KeyPackage, PublicKeyPackage};
use frost::round1::{SigningCommitments, SigningNonces};
use frost::round2::SignatureShare;
use std::collections::BTreeMap;
use ed25519_dalek::{Signature, Verifier, PublicKey};
use bs58;

// ========= CLI definition =========
#[derive(Parser)]
#[command(name = "frost-ed25519-cli", version, about = "Simple FROST-Ed25519 DKG + threshold signing demo", long_about = None)]
struct Cli {
    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Run a trusted-dealer DKG for n=3, t=2 and write share files
    Dkg {
        /// Output directory (defaults to current dir)
        #[arg(long, default_value = ".")]
        out_dir: PathBuf,
    },
    /// Sign a message with 2 shares (threshold = 2)
    Sign {
        /// Path to first share JSON (e.g. s1.json)
        share1: PathBuf,
        /// Path to second share JSON (e.g. s3.json)
        share2: PathBuf,
        /// Message to sign (hex)
        message_hex: String,
    },
    /// Verify a signature produced by this CLI or the Go demo
    Verify {
        /// Path to group_public_key.json
        pubkey_json: PathBuf,
        /// Message that was signed (hex)
        message_hex: String,
        /// Signature in hex (64-byte Ed25519)
        signature_hex: String,
    },
}

#[derive(Serialize, Deserialize)]
struct StoredShare {
    participant_index: u16,
    key_package: KeyPackage,
}

fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();

    match cli.command {
        Commands::Dkg { out_dir } => cmd_dkg(out_dir)?,
        Commands::Sign {
            share1,
            share2,
            message_hex,
        } => cmd_sign(share1, share2, &message_hex)?,
        Commands::Verify { pubkey_json, message_hex, signature_hex } => cmd_verify(pubkey_json, &message_hex, &signature_hex)?,
    }

    Ok(())
}

fn cmd_dkg(out_dir: PathBuf) -> anyhow::Result<()> {
    let (shares, pubkey_package) = frost::keys::generate_with_dealer(
        3, // n
        2, // t
        frost::keys::IdentifierList::Default,
        &mut OsRng,
    )?;

    let mut key_packages: BTreeMap<_, _> = BTreeMap::new();
    for (id, secret_share) in shares {
        let kp = frost::keys::KeyPackage::try_from(secret_share)?;
        key_packages.insert(id, kp);
    }

    fs::create_dir_all(&out_dir)?;

    // Write each share
    let mut idx_counter: u16 = 1;
    for (_, kp) in &key_packages {
        let stored = StoredShare {
            participant_index: idx_counter,
            key_package: kp.clone(),
        };
        let fname = format!("s{}.json", idx_counter);
        let path = out_dir.join(fname);
        fs::write(path, serde_json::to_vec_pretty(&stored)?)?;

        idx_counter += 1;
    }

    // Augment with base58 address for convenience
    #[derive(Serialize)]
    struct PubOut<'a> {
        #[serde(flatten)]
        inner: &'a PublicKeyPackage,
        address_base58: String,
        public_key_hex: String,
    }
    let pk_bytes = pubkey_package.verifying_key().serialize()?;
    let pub_out = PubOut {
        inner: &pubkey_package,
        address_base58: bs58::encode(&pk_bytes).into_string(),
        public_key_hex: hex::encode(&pk_bytes),
    };

    let pub_path = out_dir.join("group_public_key.json");
    fs::write(pub_path, serde_json::to_vec_pretty(&pub_out)?)?;

    println!("✅ DKG complete. Wrote shares and group public key to {:?}", out_dir);
    Ok(())
}

fn cmd_sign(share1_path: PathBuf, share2_path: PathBuf, message_hex: &str) -> anyhow::Result<()> {
    // Load shares
    let s1_bytes = fs::read(&share1_path)?;
    let s1: StoredShare = serde_json::from_slice(&s1_bytes)?;
    let s2: StoredShare = serde_json::from_slice(&fs::read(&share2_path)?)?;

    // Parse message
    let msg_bytes = hex::decode(message_hex.trim())?;

    // Generate nonces & commitments
    let nonce1 = SigningNonces::new(&s1.key_package.signing_share(), &mut OsRng);
    let nonce2 = SigningNonces::new(&s2.key_package.signing_share(), &mut OsRng);

    let comm1 = SigningCommitments::from(&nonce1);
    let comm2 = SigningCommitments::from(&nonce2);

    let mut comm_map: BTreeMap<_, _> = BTreeMap::new();
    comm_map.insert(*s1.key_package.identifier(), comm1);
    comm_map.insert(*s2.key_package.identifier(), comm2);

    let signing_package = frost::SigningPackage::new(comm_map, &msg_bytes);

    // Each participant creates signing share
    let share1: SignatureShare = frost::round2::sign(&signing_package, &nonce1, &s1.key_package)?;
    let share2: SignatureShare = frost::round2::sign(&signing_package, &nonce2, &s2.key_package)?;

    // Combine signature shares
    let mut share_map: BTreeMap<_, _> = BTreeMap::new();
    share_map.insert(*s1.key_package.identifier(), share1);
    share_map.insert(*s2.key_package.identifier(), share2);

    // Load public key package from same folder as share1 (group_public_key.json)
    let pub_dir = share1_path.parent().map(PathBuf::from).unwrap_or_else(|| PathBuf::from("."));
    let pub_path = pub_dir.join("group_public_key.json");
    let pub_data = fs::read(&pub_path)?;
    let pubkey_package: PublicKeyPackage = serde_json::from_slice(&pub_data)?;

    let group_signature = frost::aggregate(&signing_package, &share_map, &pubkey_package)?;

    // Serialize and output signature in hex
    let sig_bytes = group_signature.serialize()?;
    println!("{}", hex::encode(sig_bytes));
    Ok(())
}

fn cmd_verify(pub_path: PathBuf, message_hex: &str, sig_hex: &str) -> anyhow::Result<()> {
    let pub_data = fs::read(pub_path)?;
    let pub_pkg: PublicKeyPackage = serde_json::from_slice(&pub_data)?;

    // Extract 32-byte group public key
    let vk_bytes = pub_pkg.verifying_key().serialize()?;
    let vk = PublicKey::from_bytes(&vk_bytes)?;

    let msg = hex::decode(message_hex.trim())?;
    let sig_bytes = hex::decode(sig_hex.trim())?;
    let sig = Signature::from_bytes(&sig_bytes)?;

    match vk.verify_strict(&msg, &sig) {
        Ok(_) => println!("✅ Signature verified"),
        Err(e) => println!("❌ Verification failed: {}", e),
    }
    Ok(())
} 