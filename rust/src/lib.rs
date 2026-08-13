use std::collections::BTreeMap;
use std::fmt::{Display, Formatter};
use std::io::{self, Cursor, Read, Write};

use arrow_array::RecordBatch;
use arrow_ipc::reader::StreamReader;
use arrow_ipc::writer::StreamWriter;
use serde::{Deserialize, Serialize};

pub const ABI_VERSION: &str = "dbminer.runner.v1alpha1";
const MAGIC: &[u8; 4] = b"DBM\x01";
const CONTROL: u8 = 1;
const ARROW_BATCH: u8 = 2;

#[derive(Debug)]
pub enum Error {
    Protocol(String),
    Io(io::Error),
    Arrow(arrow_schema::ArrowError),
}

impl Display for Error {
    fn fmt(&self, formatter: &mut Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::Protocol(message) => formatter.write_str(message),
            Self::Io(error) => Display::fmt(error, formatter),
            Self::Arrow(error) => Display::fmt(error, formatter),
        }
    }
}

impl From<io::Error> for Error {
    fn from(value: io::Error) -> Self {
        Self::Io(value)
    }
}
impl From<arrow_schema::ArrowError> for Error {
    fn from(value: arrow_schema::ArrowError) -> Self {
        Self::Arrow(value)
    }
}

#[derive(Debug, Default)]
pub struct Context {
    pub execution_id: String,
    pub node_id: String,
    pub config: BTreeMap<String, String>,
}

pub trait Runner {
    fn on_initialize(&mut self, _ctx: &Context) -> Result<(), Error> {
        Ok(())
    }
    fn on_input_batch(
        &mut self,
        execution_id: &str,
        node_id: &str,
        batch: &RecordBatch,
    ) -> Result<RecordBatch, Error>;
}

#[derive(Debug, Default, Serialize, Deserialize)]
struct ControlMessage {
    abi: String,
    #[serde(rename = "type")]
    kind: String,
    #[serde(default)]
    execution_id: String,
    #[serde(default)]
    node_id: String,
    #[serde(default)]
    port_id: String,
    #[serde(default)]
    config: BTreeMap<String, String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<Failure>,
}

#[derive(Debug, Serialize, Deserialize)]
struct Failure {
    code: String,
    message: String,
}

#[derive(Debug, Default, Serialize, Deserialize)]
struct BatchMetadata {
    abi: String,
    #[serde(rename = "type")]
    kind: String,
    #[serde(default)]
    execution_id: String,
    #[serde(default)]
    node_id: String,
    #[serde(default)]
    port_id: String,
    #[serde(default)]
    edge_id: String,
    #[serde(default)]
    sequence: u64,
}

enum Frame {
    Control(ControlMessage),
    Batch(BatchMetadata, RecordBatch),
}

pub fn run(runner: &mut impl Runner) -> ! {
    if let Err(error) = run_session(runner) {
        let _ = write_control(&ControlMessage {
            abi: ABI_VERSION.into(),
            kind: "failed".into(),
            error: Some(Failure {
                code: "runner_failed".into(),
                message: error.to_string(),
            }),
            ..Default::default()
        });
        eprintln!("{error}");
        std::process::exit(1);
    }
    std::process::exit(0)
}

fn run_session(runner: &mut impl Runner) -> Result<(), Error> {
    let initialize = match read_frame()? {
        Frame::Control(message) if message.kind == "initialize" => message,
        _ => return Err(Error::Protocol("runner expected initialize".into())),
    };
    if initialize.abi != ABI_VERSION {
        return Err(Error::Protocol(format!(
            "unsupported runner ABI {:?}",
            initialize.abi
        )));
    }
    let context = Context {
        execution_id: initialize.execution_id,
        node_id: initialize.node_id,
        config: initialize.config,
    };
    runner.on_initialize(&context)?;
    write_control(&control("ready", &context))?;
    loop {
        match read_frame()? {
            Frame::Control(message) if message.kind == "input_end" => {
                write_control(&control("completed", &context))?;
                return Ok(());
            }
            Frame::Batch(metadata, batch) if metadata.kind == "input_batch" => {
                let output =
                    runner.on_input_batch(&context.execution_id, &context.node_id, &batch)?;
                write_batch(
                    BatchMetadata {
                        abi: ABI_VERSION.into(),
                        kind: "output_batch".into(),
                        execution_id: context.execution_id.clone(),
                        node_id: context.node_id.clone(),
                        port_id: "output".into(),
                        edge_id: metadata.edge_id,
                        sequence: metadata.sequence,
                    },
                    &output,
                )?;
            }
            _ => {
                return Err(Error::Protocol(
                    "runner received an unsupported message".into(),
                ))
            }
        }
    }
}

fn control(kind: &str, context: &Context) -> ControlMessage {
    ControlMessage {
        abi: ABI_VERSION.into(),
        kind: kind.into(),
        execution_id: context.execution_id.clone(),
        node_id: context.node_id.clone(),
        ..Default::default()
    }
}

fn read_frame() -> Result<Frame, Error> {
    let mut header = [0_u8; 9];
    io::stdin().read_exact(&mut header)?;
    if &header[..4] != MAGIC {
        return Err(Error::Protocol("invalid runner transport magic".into()));
    }
    let length = u32::from_be_bytes(header[5..9].try_into().unwrap()) as usize;
    if length == 0 || length > 8 << 20 {
        return Err(Error::Protocol("invalid runner frame length".into()));
    }
    let mut payload = vec![0_u8; length];
    io::stdin().read_exact(&mut payload)?;
    match header[4] {
        CONTROL => {
            let message = ciborium::from_reader(payload.as_slice())
                .map_err(|error| Error::Protocol(format!("decode control message: {error}")))?;
            Ok(Frame::Control(message))
        }
        ARROW_BATCH => {
            if payload.len() < 5 {
                return Err(Error::Protocol("truncated Arrow batch frame".into()));
            }
            let metadata_length = u32::from_be_bytes(payload[..4].try_into().unwrap()) as usize;
            if metadata_length == 0 || 4 + metadata_length >= payload.len() {
                return Err(Error::Protocol("invalid Arrow metadata length".into()));
            }
            let metadata = ciborium::from_reader(&payload[4..4 + metadata_length])
                .map_err(|error| Error::Protocol(format!("decode batch metadata: {error}")))?;
            let mut reader =
                StreamReader::try_new(Cursor::new(&payload[4 + metadata_length..]), None)?;
            let batch = reader
                .next()
                .ok_or_else(|| Error::Protocol("Arrow frame contains no batch".into()))??;
            if reader.next().is_some() {
                return Err(Error::Protocol(
                    "Arrow frame contains multiple batches".into(),
                ));
            }
            Ok(Frame::Batch(metadata, batch))
        }
        kind => Err(Error::Protocol(format!("unknown runner frame kind {kind}"))),
    }
}

fn write_control(message: &ControlMessage) -> Result<(), Error> {
    let mut payload = Vec::new();
    ciborium::into_writer(message, &mut payload)
        .map_err(|error| Error::Protocol(format!("encode control message: {error}")))?;
    write_frame(CONTROL, &payload)
}

fn write_batch(metadata: BatchMetadata, batch: &RecordBatch) -> Result<(), Error> {
    let mut metadata_bytes = Vec::new();
    ciborium::into_writer(&metadata, &mut metadata_bytes)
        .map_err(|error| Error::Protocol(format!("encode batch metadata: {error}")))?;
    let mut arrow_bytes = Vec::new();
    {
        let mut writer = StreamWriter::try_new(&mut arrow_bytes, &batch.schema())?;
        writer.write(batch)?;
        writer.finish()?;
    }
    let mut payload = Vec::with_capacity(4 + metadata_bytes.len() + arrow_bytes.len());
    payload.extend_from_slice(&(metadata_bytes.len() as u32).to_be_bytes());
    payload.extend_from_slice(&metadata_bytes);
    payload.extend_from_slice(&arrow_bytes);
    write_frame(ARROW_BATCH, &payload)
}

fn write_frame(kind: u8, payload: &[u8]) -> Result<(), Error> {
    let mut output = io::stdout().lock();
    output.write_all(MAGIC)?;
    output.write_all(&[kind])?;
    output.write_all(&(payload.len() as u32).to_be_bytes())?;
    output.write_all(payload)?;
    output.flush()?;
    Ok(())
}
