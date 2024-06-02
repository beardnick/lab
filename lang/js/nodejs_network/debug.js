
function traceEvents(emitter) {
	let oldEmit = emitter.emit;
	if (emitter._emitTraced) {
		return;
	}
	const tracer = {
		events: [],
	};
	emitter.emit = function () {
		tracer.events.push(arguments[0]);
		oldEmit.apply(emitter, arguments);
	}
	emitter._emitTraced = true;
	return tracer;
}

// monkey patch emitter so that we can trace emitted events
function patchEmitter(emitter, opt = { thisName: null, traceLevel: -1 }) {
	if (!opt.thisName) {
		opt.thisName = Object.getPrototypeOf(emitter)?.constructor?.name;
	}
	let oldEmit = emitter.emit;
	if (emitter._emitTracerPatched) {
		return;
	}
	emitter.emit = function () {
		// const traces = (new Error().stack);
		const traces = (new Error().stack);
		let traceStr = '';
		if (opt.traceLevel === -1) {
			traceStr = traces ?? '';
		} else if (opt.traceLevel > 0) {
			traceStr = traces?.split('\n')[opt.traceLevel] ?? '';
		}
		console.log(`${traceStr} ${opt.thisName} emit event ${arguments[0]}`);
		// console.log(`${traces} ${thisName} emit event ${arguments[0]}`);
		// console.log(`${thisName} emit event ${arguments[0]}`);
		oldEmit.apply(emitter, arguments);
	}
	emitter._emitTracerPatched = true;
}

module.exports = {
	patchEmitter,
	traceEvents,
};