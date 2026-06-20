use portable_pty::{native_pty_system, Child, CommandBuilder, PtyPair, PtySize};
use std::io::{Read, Write};
use std::sync::{Arc, Mutex};

#[allow(dead_code)]
pub struct PtySession {
    writer: Arc<Mutex<Box<dyn Write + Send>>>,
    pair: Mutex<PtyPair>,
    child: Box<dyn Child + Send + Sync>,
}

#[allow(dead_code)]
impl PtySession {
    pub fn new(cols: u32, rows: u32) -> Result<Self, String> {
        let pty_system = native_pty_system();

        let pair = pty_system
            .openpty(PtySize {
                rows: rows as u16,
                cols: cols as u16,
                pixel_width: 0,
                pixel_height: 0,
            })
            .map_err(|e| format!("打开 PTY 失败: {}", e))?;

        let shell = detect_shell();
        #[allow(unused_mut)]
        let mut cmd = CommandBuilder::new(shell);

        #[cfg(unix)]
        cmd.env("TERM", "xterm-256color");

        // Spawn shell
        let child = pair
            .slave
            .spawn_command(cmd)
            .map_err(|e| format!("启动终端 Shell 失败: {}", e))?;

        let writer = pair
            .master
            .take_writer()
            .map_err(|e| format!("获取 PTY 写入流失败: {}", e))?;

        Ok(PtySession {
            writer: Arc::new(Mutex::new(writer)),
            pair: Mutex::new(pair),
            child,
        })
    }

    pub fn write(&self, data: &[u8]) -> Result<(), String> {
        let mut w = self
            .writer
            .lock()
            .map_err(|_| "PTY 写入锁错误".to_string())?;
        w.write_all(data)
            .map_err(|e| format!("PTY 写入失败: {}", e))?;
        w.flush().map_err(|e| format!("PTY 刷新失败: {}", e))?;
        Ok(())
    }

    pub fn resize(&self, cols: u32, rows: u32) -> Result<(), String> {
        let pair = self.pair.lock().map_err(|_| "PTY 锁错误".to_string())?;
        pair.master
            .resize(PtySize {
                rows: rows as u16,
                cols: cols as u16,
                pixel_width: 0,
                pixel_height: 0,
            })
            .map_err(|e| format!("PTY 改变尺寸失败: {}", e))
    }

    pub fn try_clone_reader(&self) -> Result<Box<dyn Read + Send>, String> {
        let pair = self.pair.lock().map_err(|_| "PTY 锁错误".to_string())?;
        pair.master
            .try_clone_reader()
            .map_err(|e| format!("获取 PTY 读取流失败: {}", e))
    }

    pub fn kill(&mut self) {
        let _ = self.child.kill();
    }
}

impl Drop for PtySession {
    fn drop(&mut self) {
        let _ = self.child.kill();
    }
}

fn detect_shell() -> String {
    if cfg!(target_os = "windows") {
        if std::path::Path::new("C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe")
            .exists()
        {
            "powershell.exe".to_string()
        } else {
            "cmd.exe".to_string()
        }
    } else {
        // Unix shell detection
        let shells = vec!["/bin/zsh", "/bin/bash", "/bin/sh"];
        for sh in shells {
            if std::path::Path::new(sh).exists() {
                return sh.to_string();
            }
        }
        "/bin/sh".to_string()
    }
}
