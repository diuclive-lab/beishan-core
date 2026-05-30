use serde::{Deserialize, Serialize};
use std::io::{self, BufRead, Write};
use std::process::{Child, Command};
use std::sync::Mutex;

#[derive(Deserialize)]
struct Request {
    id: u64,
    method: String,
    params: Option<serde_json::Value>,
}

#[derive(Serialize)]
struct Response {
    id: u64,
    #[serde(skip_serializing_if = "Option::is_none")]
    result: Option<serde_json::Value>,
    #[serde(skip_serializing_if = "Option::is_none")]
    error: Option<String>,
}

struct ServoInstance {
    process: Child,
    base_url: String,
    session_id: String,
}

static SERVO: Mutex<Option<ServoInstance>> = Mutex::new(None);

fn main() {
    let stdin = io::stdin();
    let mut reader = io::BufReader::new(stdin.lock());
    let mut buf = Vec::new();

    loop {
        buf.clear();
        match reader.read_until(b'\0', &mut buf) {
            Ok(0) | Err(_) => break,
            Ok(_) => {}
        }
        let data = &buf[..buf.len().saturating_sub(1)];
        if data.is_empty() {
            continue;
        }
        let req: Request = match serde_json::from_slice(data) {
            Ok(r) => r,
            Err(e) => {
                let resp = Response { id: 0, result: None, error: Some(format!("parse error: {}", e)) };
                write_resp(&resp);
                continue;
            }
        };
        let resp = handle(&req);
        write_resp(&resp);
    }
}

fn write_resp(resp: &Response) {
    if let Ok(data) = serde_json::to_vec(resp) {
        let mut out = io::stdout().lock();
        let _ = out.write_all(&data);
        let _ = out.write_all(b"\0");
        let _ = out.flush();
    }
}

fn handle(req: &Request) -> Response {
    match req.method.as_str() {
        "ping" => ok(req.id, serde_json::json!({"pong": true})),
        "start" => cmd_start(req),
        "close" => cmd_close(req),
        _ => {
            // All other commands need an active instance
            let sid = {
                let guard = SERVO.lock().unwrap();
                guard.as_ref().map(|i| (i.base_url.clone(), i.session_id.clone()))
            };
            let (base_url, session_id) = match sid {
                Some(s) => s,
                None => return err(req.id, "Servo not started"),
            };
            match req.method.as_str() {
                "navigate" => cmd_navigate(req, &base_url, &session_id),
                "eval" => cmd_eval(req, &base_url, &session_id),
                "inner_text" => cmd_inner_text(req, &base_url, &session_id),
                "screenshot" => cmd_screenshot(req, &base_url, &session_id),
                _ => err(req.id, &format!("unknown: {}", req.method)),
            }
        }
    }
}

fn cmd_start(req: &Request) -> Response {
    let mut guard = SERVO.lock().unwrap();
    if guard.is_some() {
        return err(req.id, "already started");
    }
    let servo_path = std::env::var("BEISHAN_SERVO").unwrap_or_else(|_| {
        "/Users/dc/Desktop/cankaocangku/servo/target/release/servoshell".to_string()
    });
    let port = free_port();
    let base_url = format!("http://127.0.0.1:{}", port);

    let mut child = match Command::new(&servo_path)
        .arg("--headless")
        .arg(format!("--webdriver={}", port))
        .stderr(std::process::Stdio::null())
        .spawn()
    {
        Ok(c) => c,
        Err(e) => return err(req.id, &format!("start: {}", e)),
    };

    let mut ready = false;
    for _ in 0..20 {
        if let Ok(r) = ureq::get(&format!("{}/status", base_url)).call() {
            if r.status() == 200 { ready = true; break; }
        }
        std::thread::sleep(std::time::Duration::from_millis(500));
    }
    if !ready {
        let _ = child.wait();
        return err(req.id, "Servo WebDriver not ready");
    }

    let caps = serde_json::json!({
        "capabilities": { "alwaysMatch": { "browserName": "servo" } }
    });
    let sid = match ureq::post(&format!("{}/session", base_url))
        .set("Content-Type", "application/json")
        .send_json(&caps)
    {
        Ok(r) => {
            let v: serde_json::Value = r.into_json().unwrap_or_default();
            v["value"]["sessionId"].as_str().unwrap_or("").to_string()
        }
        Err(_) => {
            let _ = child.wait();
            return err(req.id, "create session failed");
        }
    };
    if sid.is_empty() {
        let _ = child.wait();
        return err(req.id, "empty sessionId");
    }

    *guard = Some(ServoInstance { process: child, base_url: base_url.clone(), session_id: sid.clone() });
    ok(req.id, serde_json::json!({"status":"started","session":sid}))
}

fn wd_post(base_url: &str, session_id: &str, path: &str, body: &serde_json::Value) -> Result<serde_json::Value, String> {
    let url = format!("{}/session/{}/{}", base_url, session_id, path);
    let resp = ureq::post(&url)
        .set("Content-Type", "application/json")
        .send_json(body)
        .map_err(|e| format!("wd post: {}", e))?;
    let val: serde_json::Value = resp.into_json().map_err(|e| format!("json: {}", e))?;
    Ok(val["value"].clone())
}

fn cmd_navigate(_req: &Request, base: &str, sid: &str) -> Response {
    let url = _req.params.as_ref().and_then(|p| p["url"].as_str()).unwrap_or("");
    if url.is_empty() { return err(_req.id, "url required"); }
    let body = serde_json::json!({"url": url});
    match wd_post(base, sid, "url", &body) {
        Ok(_) => ok(_req.id, serde_json::json!({"navigated": url})),
        Err(e) => err(_req.id, &e),
    }
}

fn cmd_eval(_req: &Request, base: &str, sid: &str) -> Response {
    let script = _req.params.as_ref().and_then(|p| p["script"].as_str()).unwrap_or("");
    if script.is_empty() { return err(_req.id, "script required"); }
    let body = serde_json::json!({"script": script, "args": []});
    match wd_post(base, sid, "execute/sync", &body) {
        Ok(v) => ok(_req.id, serde_json::json!({"result": v})),
        Err(e) => err(_req.id, &e),
    }
}

fn cmd_inner_text(_req: &Request, base: &str, sid: &str) -> Response {
    let body = serde_json::json!({"script": "return document.body.innerText", "args": []});
    match wd_post(base, sid, "execute/sync", &body) {
        Ok(v) => {
            let text = v.as_str().unwrap_or("").to_string();
            ok(_req.id, serde_json::json!({"text": text}))
        }
        Err(e) => err(_req.id, &e),
    }
}

fn cmd_screenshot(_req: &Request, base: &str, sid: &str) -> Response {
    let url = format!("{}/session/{}/screenshot", base, sid);
    match ureq::get(&url).call() {
        Ok(r) => {
            let v: serde_json::Value = r.into_json().unwrap_or_default();
            let data = v["value"].as_str().unwrap_or("").to_string();
            ok(_req.id, serde_json::json!({"data": data}))
        }
        Err(e) => err(_req.id, &format!("screenshot: {}", e)),
    }
}

fn cmd_close(_req: &Request) -> Response {
    let mut guard = SERVO.lock().unwrap();
    if let Some(mut inst) = guard.take() {
        let _ = ureq::delete(&format!("{}/session/{}", inst.base_url, inst.session_id)).call();
        let _ = inst.process.wait();
    }
    ok(_req.id, serde_json::json!({"status":"closed"}))
}

fn ok(id: u64, v: serde_json::Value) -> Response {
    Response { id, result: Some(v), error: None }
}
fn err(id: u64, msg: &str) -> Response {
    Response { id, result: None, error: Some(msg.to_string()) }
}
fn free_port() -> u16 {
    std::net::TcpListener::bind("127.0.0.1:0")
        .and_then(|l| l.local_addr().map(|a| a.port()))
        .unwrap_or(9222)
}
