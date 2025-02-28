build:
	cargo build
	rust-objcopy --strip-all target/riscv64gc-unknown-none-elf/debug/toyos -O binary target/riscv64gc-unknown-none-elf/debug/toyos.bin

run:
	qemu-system-riscv64 \
		-machine virt \
		-nographic \
		-bios  bootloader/rustsbi-qemu.bin \
		-device loader,file=target/riscv64gc-unknown-none-elf/debug/toyos.bin,addr=0x80200000

rungui:
	qemu-system-riscv64 \
		-bios  bootloader/rustsbi-qemu.bin \
		-device loader,file=target/riscv64gc-unknown-none-elf/debug/toyos.bin,addr=0x80200000

debug:
	qemu-system-riscv64 \
		-machine virt \
		-nographic \
		-bios  bootloader/rustsbi-qemu.bin \
		-device loader,file=target/riscv64gc-unknown-none-elf/debug/toyos.bin,addr=0x80200000 \
		-s -S
