const http = require('http');
const httpx = require('../httpx');
const debugx = require('../debug');
const url = require('url');
const { hostname } = require('os');


describe('http events', () => {
	const testCase = async (c) => {
		const server = httpx.createServer(c.serverOpt);
		const tracer = debugx.traceEvents(server);
		server.on('request', httpx.onRequest);
		// the "upgrade" event is triggered only when the user has started listening to it.
		server.on('upgrade', httpx.onUpgrade);
		const port = await httpx.getFreePort();

		await server.promise.listen(port);
		const u = new URL(c.requestOpt.url);
		u.port = port;
		console.log(port);
		c.requestOpt.url = u.toString();
		await httpx.request(c.requestOpt);
		await server.promise.close();

		expect(tracer?.events).toStrictEqual(c.wantEvents);
	};

	it(
		'should invoke events in http request',
		async () => {
			await testCase(
				{
					serverOpt: {
						https: false,
						http2: false
					},
					requestOpt: {
						url: 'http://127.0.0.1:8000/hello/world',
					},
					wantEvents: ['listening', 'connection', 'request', 'close'],
				},
			);
		},
	);

	it(
		'should invoke events in https request',
		async () => {
			await testCase({
				serverOpt: {
					https: true,
					http2: false
				},
				requestOpt: {
					rejectUnauthorized: false,
					url: 'https://127.0.0.1:8000/hello/world',
				},
				wantEvents: ['listening', 'connection', 'secureConnection', 'request', 'close'],
			});
		},
	);
	it(
		'should invoke events in http2 request',
		async () => {
			await testCase(
				{
					serverOpt: {
						https: true,
						http2: true
					},
					requestOpt: {
						http2: true,
						rejectUnauthorized: false,
						url: 'https://127.0.0.1:8000/hello/world',
					},
					wantEvents: ['newListener', 'listening', 'connection', 'secureConnection', 'session', 'stream', 'request', 'close'],
				}
			);
		},
	);

	it(
		'should invoke events in websocket request',
		async () => {
			await testCase(
				{
					serverOpt: {
						https: false,
						http2: false
					},
					requestOpt: {
						rejectUnauthorized: false,
						url: 'http://127.0.0.1:8000/hello/world',
						headers: {
							'Connection': 'Upgrade',
							'Upgrade': 'websocket',
						},
					},
					wantEvents: ['listening', 'connection', 'upgrade', 'close'],
				}
			);
		},
	);
});




