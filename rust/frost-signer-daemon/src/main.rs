use std::{collections::HashMap, net::SocketAddr, sync::Arc};

use axum::{
    extract::State,
    routing::{post},
    Json, Router,
};
use anyhow::{anyhow, Result};
use frost_ed25519 as frost;
use frost::round1::{SigningCommitments, SigningNonces};
use frost::round2::SignatureShare;
use frost::{keys::KeyPackage, SigningPackage};
use rand::rngs::OsRng;
use serde::{Deserialize, Serialize};
use tokio::sync::Mutex;
use tracing::{info, instrument};

/// Wrapper type stored on disk – identical to what the DKG JSON exported.
#[derive(Deserialize, Debug)]
struct StoredShare {
    key_package: KeyPackage,
}

/// JSON body for /nonce request.
#[derive(Deserialize, Debug)]
struct NonceRequest {
    /// Message to be signed (hex-encoded).
    message: String,
}

/// Response: our participant id and commitments.
#[derive(Serialize)]
struct NonceResponse {
    participant_id: String,
    commitments: SigningCommitments,
}

/// JSON body for /sign request.
#[derive(Deserialize, Debug)]
struct SignRequest {
    /// Frost signing package produced by coordinator (serde JSON).
    package: SigningPackage,
}

/// Response: signature share (serde JSON serialisation).
#[derive(Serialize)]
struct SignResponse {
    share: SignatureShare,
}

/// Per-message cached nonces so that we can use them in round 2.
struct Cached;

type MsgId = String; // we’ll use message hex as ID

#[derive(Debug)]
struct AppState {
    signing_key_pkg: KeyPackage,
    nonces: Mutex<HashMap<MsgId, (SigningNonces, SigningCommitments)>>,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt::init();

    let share_path = std::env::args()
        .nth(1)
        .ok_or_else(|| anyhow!("usage: frost-signer-daemon <share.json> [addr]"))?;
    let addr: SocketAddr = std::env::args()
        .nth(2)
        .unwrap_or_else(|| "127.0.0.1:3000".to_string())
        .parse()?;

    // load share file
    let stored: StoredShare = serde_json::from_reader(std::fs::File::open(&share_path)?)?;

    let state = Arc::new(AppState {
        signing_key_pkg: stored.key_package,
        nonces: Mutex::new(HashMap::new()),
    });

    let app = Router::new()
        .route("/nonce", post(handle_nonce))
        .route("/sign", post(handle_sign))
        .with_state(state);

    info!("listening on {}", addr);
    axum::Server::bind(&addr)
        .serve(app.into_make_service())
        .await?;

    Ok(())
}

#[instrument]
async fn handle_nonce(State(state): State<Arc<AppState>>, Json(body): Json<NonceRequest>) -> Result<Json<NonceResponse>, (axum::http::StatusCode, String)> {
    if hex::decode(&body.message).is_err() {
        return Err((axum::http::StatusCode::BAD_REQUEST, "invalid hex".to_string()));
    }

    let mut nonces_map = state.nonces.lock().await;
    if let Some((_, commitments)) = nonces_map.get(&body.message) {
        // Already generated – return same commitments (idempotent)
        return Ok(Json(NonceResponse {
            participant_id: hex::encode(state.signing_key_pkg.identifier().serialize()),
            commitments: *commitments,
        }));
    }

    // Generate new nonces & commitments
    let signing_share = state.signing_key_pkg.signing_share();
    let (signing_nonces, signing_commitments) = frost::round1::commit(signing_share, &mut OsRng);

    nonces_map.insert(body.message.clone(), (signing_nonces, signing_commitments));

    Ok(Json(NonceResponse {
        participant_id: hex::encode(state.signing_key_pkg.identifier().serialize()),
        commitments: signing_commitments,
    }))
}

#[instrument]
async fn handle_sign(State(state): State<Arc<AppState>>, Json(body): Json<SignRequest>) -> Result<Json<SignResponse>, (axum::http::StatusCode, String)> {
    // Serialize package to get message identifier (hex of message)
    let msg_hex = hex::encode(body.package.message());

    let (signing_nonces, _) = {
        let mut nonces_map = state.nonces.lock().await;
        nonces_map
            .remove(&msg_hex)
            .ok_or((axum::http::StatusCode::BAD_REQUEST, "nonce not found".to_string()))?
    };

    // Compute signature share
    let sig_share = frost::round2::sign(&body.package, &signing_nonces, &state.signing_key_pkg)
        .map_err(|e| (axum::http::StatusCode::INTERNAL_SERVER_ERROR, format!("sign error: {e:?}")))?;

    Ok(Json(SignResponse { share: sig_share }))
} 