use std::fs::File;
use std::io::prelude::*;

fn main() {
    let mut file = File::create("foo.txt").unwrap();

    let u = "0\n".to_string();
    let mut i: u64 = 0;

    while i < 10000 {
      file.write_all(u.as_bytes()).unwrap();
      i += 1
    }

    file.flush().unwrap();
}
