use anyhow::{anyhow, Result};
use clap::Parser;
use serde::Deserialize;
use solana_client::rpc_client::RpcClient;
use solana_sdk::{pubkey::Pubkey, system_instruction, transaction::Transaction};
use solana_sdk::signature::Signature as SolSignature;
use solana_sdk::message::Message;
use std::path::PathBuf;

use curve25519_dalek::constants::ED25519_BASEPOINT_TABLE;
use curve25519_dalek::scalar::Scalar;
use curve25519_dalek::edwards::EdwardsPoint;
use rand::rngs::OsRng;
use rand::RngCore;
use sha2::{Digest, Sha512};
use std::convert::TryFrom;
// we no longer reconstruct keys from shares inside this binary

#[derive(Parser, Debug)]
#[command(author, version, about, long_about = None)]
struct Args {
    // no share files needed anymore

    #[arg(long)]
    to: Pubkey,

    #[arg(long, default_value_t = 1_000_000)]
    lamports: u64,

    #[arg(long, default_value = "https://api.devnet.solana.com")]
    rpc: String,

    /// Path to 64-byte JSON keypair (created by frost-keygen)
    #[arg(value_name = "KEYPAIR_PATH")]
    keypair_path: PathBuf,
}

#[derive(Debug, Deserialize)]
#[serde(untagged)]
enum ShareFile {
    Simple {
        index: u8,
        share_hex: String,
    },
    Frost {
        participant_index: u8,
        key_package: FrostKeyPackage,
    },
}

#[derive(Debug, Deserialize)]
struct FrostKeyPackage {
    signing_share: String,
}

// Replace existing main implementation with unified logic
fn main() -> Result<()> {
    let args = Args::parse();
    let client = RpcClient::new(args.rpc);

    // Read keypair bytes (Vec<u8>)
    let kp_bytes: Vec<u8> = serde_json::from_reader(std::fs::File::open(&args.keypair_path)?)?;
    if kp_bytes.len() < 32 {
        return Err(anyhow!("keypair file must contain at least 32 bytes (secret scalar)"));
    }
    let sk_bytes: [u8; 32] = kp_bytes[..32].try_into().unwrap();

    // Interpret as scalar (little-endian, already clamped by FROST)
    let secret_scalar = Scalar::from_bits(sk_bytes);

    // Compute public key point
    let pk_point: EdwardsPoint = &ED25519_BASEPOINT_TABLE * &secret_scalar;
    let pk_bytes = pk_point.compress().to_bytes();
    let sender = Pubkey::try_from(pk_bytes.as_slice())?;
    let balance = client.get_balance(&sender)?;
    println!("Sender pubkey: {}", sender);
    println!(
        "Current balance: {} lamports ({} SOL)",
        balance,
        balance as f64 / 1_000_000_000f64
    );

    if balance < args.lamports {
        return Err(anyhow!(
            "Insufficient balance: trying to send {} lamports but only have {}",
            args.lamports,
            balance
        ));
    }

    let ix = system_instruction::transfer(&sender, &args.to, args.lamports);
    let recent_hash = client.get_latest_blockhash()?;

    let message = Message::new(&[ix], Some(&sender));
    let mut tx = Transaction::new_unsigned(message);
    tx.message.recent_blockhash = recent_hash;

    let msg_bytes = tx.message.serialize();

    // Ed25519 Schnorr signature (similar to Ed25519):
    // Generate random nonce r
    let mut rng = OsRng;
    let mut r_bytes = [0u8; 64];
    rng.fill_bytes(&mut r_bytes);
    let r_scalar = Scalar::from_bytes_mod_order(r_bytes[..32].try_into().unwrap());
    let R_point = &ED25519_BASEPOINT_TABLE * &r_scalar;
    let R_bytes = R_point.compress().to_bytes();

    // Compute challenge c = H(R || pk || m) mod L
    let mut hasher = Sha512::new();
    hasher.update(R_bytes);
    hasher.update(pk_bytes);
    hasher.update(&msg_bytes);
    let h = hasher.finalize();
    let mut h_array = [0u8; 64];
    h_array.copy_from_slice(&h);
    let c_scalar = Scalar::from_bytes_mod_order_wide(&h_array);

    let z_scalar = &r_scalar + &c_scalar * &secret_scalar;

    let mut sig_bytes = [0u8; 64];
    sig_bytes[..32].copy_from_slice(&R_bytes);
    sig_bytes[32..].copy_from_slice(&z_scalar.to_bytes());

    let sol_sig = SolSignature::from(sig_bytes);
    tx.signatures = vec![sol_sig];
    // done

    let sig_sent = client.send_and_confirm_transaction(&tx)?;
    println!("Signature: {}", sig_sent);


    Ok(())
} 