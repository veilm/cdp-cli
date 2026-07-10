#!/usr/bin/env node

const [targetPattern, device = "phone", portArg = process.env.CDP_PORT || "9222"] =
	process.argv.slice(2);

if (!targetPattern) {
	console.error(
		"Usage: set-device.mjs <tab-url-pattern> <phone|tablet|reset|WIDTHxHEIGHT> [port]",
	);
	process.exit(2);
}

const presets = {
	phone: { width: 390, height: 844, mobile: true },
	tablet: { width: 820, height: 1180, mobile: true },
};

const port = Number(portArg);
const targets = await fetch(`http://127.0.0.1:${port}/json/list`).then((response) => {
	if (!response.ok) throw new Error(`DevTools target list: HTTP ${response.status}`);
	return response.json();
});
const matches = targets.filter(
	(target) =>
		target.type === "page" &&
		target.url.includes(targetPattern) &&
		target.webSocketDebuggerUrl,
);

if (matches.length !== 1) {
	throw new Error(
		`Expected one page matching ${JSON.stringify(targetPattern)}, found ${matches.length}`,
	);
}

const ws = new WebSocket(matches[0].webSocketDebuggerUrl);
const pending = new Map();
let nextId = 1;

const call = (method, params = {}) =>
	new Promise((resolve, reject) => {
		const id = nextId++;
		pending.set(id, { resolve, reject });
		ws.send(JSON.stringify({ id, method, params }));
	});

ws.onmessage = ({ data }) => {
	const message = JSON.parse(data);
	const waiter = pending.get(message.id);
	if (!waiter) return;
	pending.delete(message.id);
	if (message.error) waiter.reject(new Error(message.error.message));
	else waiter.resolve(message.result);
};

await new Promise((resolve, reject) => {
	ws.onopen = resolve;
	ws.onerror = () => reject(new Error("Unable to open target websocket"));
});

if (device === "reset" || device === "desktop") {
	await call("Emulation.clearDeviceMetricsOverride");
	await call("Emulation.setTouchEmulationEnabled", { enabled: false });
} else {
	const custom = /^(\d+)x(\d+)$/.exec(device);
	const metrics = presets[device] ||
		(custom && {
			width: Number(custom[1]),
			height: Number(custom[2]),
			mobile: false,
		});
	if (!metrics) throw new Error(`Unknown device preset ${JSON.stringify(device)}`);

	await call("Emulation.setDeviceMetricsOverride", {
		...metrics,
		deviceScaleFactor: 1,
		screenWidth: metrics.width,
		screenHeight: metrics.height,
	});
	await call("Emulation.setTouchEmulationEnabled", {
		enabled: metrics.mobile,
		maxTouchPoints: metrics.mobile ? 5 : 1,
	});
}

const result = await call("Runtime.evaluate", {
	expression:
		"JSON.stringify({url: location.href, width: innerWidth, height: innerHeight, dpr: devicePixelRatio})",
	returnByValue: true,
});
console.log(result.result.value);
ws.close();
