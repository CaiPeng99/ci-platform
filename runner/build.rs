fn main() {
    println!("cargo:rerun-if-changed=../control-plane/proto/runner.proto");
    
    let result = tonic_build::configure()
        .build_server(false)
        .compile(
            &["../control-plane/proto/runner.proto"],
            &["../control-plane/proto"],
        );
    
    match result {
        Ok(_) => println!("Proto compilation successful!"),
        Err(e) => {
            eprintln!("Proto compilation failed: {:?}", e);
            panic!("Failed to compile protos");
        }
    }
}