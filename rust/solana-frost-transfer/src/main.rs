use anyhow::Result;
use clap::Parser;
use ed25519_dalek::{Keypair as DalekKeypair, SecretKey};
use sharks::{Sharks, Share};
use anyhow::anyhow;
use serde::Deserialize;
use solana_client::rpc_client::RpcClient;
use solana_sdk::{
    pubkey::Pubkey, signer::Signer,
    system_instruction, transaction::Transaction,
};
use solana_sdk::signature::Keypair as SolKeypair;
use std::{fs, path::PathBuf};
use std::fs::File;
use std::io::BufReader;

// removed FROST signing imports

#[derive(Parser, Debug)]
#[command(author, version, about, long_about = None)]
struct Args {
    #[arg(long, value_name = "FILE")]
    s1_path: PathBuf,

    #[arg(long, value_name = "FILE")]
    s2_path: PathBuf,

    #[arg(long)]
    to: Pubkey,

    #[arg(long, default_value_t = 1_000_000)]
    lamports: u64,

    #[arg(long, default_value = "https://api.devnet.solana.com")]
    rpc: String,
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

fn reconstruct_keypair(s1: ShareFile, s2: ShareFile) -> Result<SolKeypair> {
    let sharks = Sharks(2);

    // build raw byte vector (index || share_bytes) from ShareFile
    let build_bytes = |sf: ShareFile| -> Result<Vec<u8>> {
        let (index, hex_str) = match sf {
            ShareFile::Simple { index, share_hex } => (index, share_hex),
            ShareFile::Frost { participant_index, key_package } => (participant_index, key_package.signing_share),
        };
        let mut v = Vec::with_capacity(hex_str.len() / 2 + 1);
        v.push(index);
        v.extend_from_slice(&hex::decode(hex_str)?);
        Ok(v)
    };

    let (bytes1, bytes2) = {
        (build_bytes(s1)?, build_bytes(s2)?)
    };

    let share1 = Share::try_from(bytes1.as_slice()).map_err(|e| anyhow!(e))?;
    let share2 = Share::try_from(bytes2.as_slice()).map_err(|e| anyhow!(e))?;
    let shares_vec = vec![share1, share2];
    let secret = sharks.recover(&shares_vec).map_err(|e| anyhow!(e.to_string()))?;
    let secret_bytes: [u8; 32] = secret[..].try_into()?;
    let sk = SecretKey::from_bytes(&secret_bytes)?;
    let pk = (&sk).into();
    let dalek_kp = DalekKeypair { secret: sk, public: pk };
    let mut kp_bytes = [0u8; 64];
    kp_bytes[..32].copy_from_slice(dalek_kp.secret.as_bytes());
    kp_bytes[32..].copy_from_slice(dalek_kp.public.as_bytes());
    let sol_kp = SolKeypair::from_bytes(&kp_bytes)?;
    Ok(sol_kp)
}

// Replace existing main implementation with unified logic
fn main() -> Result<()> {
    let args = Args::parse();
    let client = RpcClient::new(args.rpc);

    // Load share files and reconstruct keypair using Shamir shares (Sharks)
    let s1: ShareFile = serde_json::from_reader(BufReader::new(File::open(args.s1_path)?))?;
    let s2: ShareFile = serde_json::from_reader(BufReader::new(File::open(args.s2_path)?))?;

    let keypair = reconstruct_keypair(s1, s2)?;
    let sender = keypair.pubkey();
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
    let tx = Transaction::new_signed_with_payer(&[ix], Some(&sender), &[&keypair], recent_hash);

    let sig_sent = client.send_and_confirm_transaction(&tx)?;
    println!("Signature: {}", sig_sent);


    Ok(())
} 