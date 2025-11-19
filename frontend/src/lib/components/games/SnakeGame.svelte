<script>
	import { onMount, onDestroy } from 'svelte';

	export let onGameOver = (score) => {};
	export let onClose = () => {};

	let canvas;
	let ctx;
	let gameLoop;
	let score = 0;
	let gameRunning = false;
	let gameOver = false;
	let gameStarted = false;
	let showHighScoreEntry = false;
	let playerName = '';
	let highScores = [];

	const gridSize = 20;
	const tileCount = 25;
	const canvasSize = gridSize * tileCount;

	// bonus items
	let bonuses = [];
	let bonusSpawnTimer = 0;
	let activeBonus = null;

	let snake = [{ x: 10, y: 10 }];
	let direction = { x: 1, y: 0 };
	let nextDirection = { x: 1, y: 0 };
	let food = [];
	let maxFood = 8; // increased to ensure all types spawn

	// hazards that kill the snake
	let hazards = [];
	let maxHazards = 12;
	let hazardSpawnTimer = 0;
	let hazardLifetime = 8000; // 8 seconds

	// wall state
	let wallsActive = false;
	let wallTimer = 0;
	let wallCycleTime = 300; // 30 seconds per cycle (more rare)
	let wallWarningTime = 30; // 3 seconds warning

	// visual effects
	let backgroundColor = '#0b2063';
	let backgroundGradient = null;
	let colorChangeTimer = 0;
	let backgroundAnimationFrame = 0;
	let currentEvent = null;
	let eventTimer = 0;
	let eventWarning = null;
	let eventWarningTimer = 0;
	let gridFlash = false;
	let wallHitGracePeriod = false;
	let graceTimer = 0;
	let ultraMode = false;

	const credTypes = [
		{ type: 'hash', color: '#5dd8c4', points: 10, emoji: '📧', glow: '#5dd8c4', growth: 1 },
		{ type: 'credential', color: '#f6287b', points: 30, emoji: '💳', glow: '#f6287b', growth: 2 },
		{ type: 'admin', color: '#9622fc', points: 50, emoji: '🔑', glow: '#9622fc', growth: 3 },
		{ type: 'privileged', color: '#ff00ff', points: 100, emoji: '₿', glow: '#ff00ff', growth: 4 }
	];

	const bonusTypes = [
		{ type: '2x', color: '#ffff00', emoji: '✨', duration: 10000, effect: '2x points' },
		{ type: 'trippy', color: '#00ffff', emoji: '🌀', duration: 5000, effect: 'rainbow mode' },
		{ type: 'shield', color: '#00ff00', emoji: '🛡️', duration: 0, effect: 'shield' },
		{ type: 'speed', color: '#ffa500', emoji: '⚡', duration: 6000, effect: 'slow motion' },
		{ type: 'supertrip', color: '#ff00ff', emoji: '🍄', duration: 8000, effect: 'psychedelic' },
		{ type: 'shrink', color: '#ff69b4', emoji: '📉', duration: 0, effect: 'shrink snake' },
		{ type: 'ghost', color: '#9370db', emoji: '👤', duration: 8000, effect: 'ghost mode' },
		{ type: 'magnet', color: '#ffd700', emoji: '🧲', duration: 10000, effect: 'food magnet' }
	];

	const backgroundGradients = [
		['#0b2063', '#1a3050', '#0b2063'],
		['#0b2063', '#2d1a4d', '#0b2063'],
		['#0b2063', '#1a4d4d', '#0b2063'],
		['#0b2063', '#4d1a2d', '#0b2063'],
		['#1a0f3d', '#0b2063', '#0f1a3d'],
		['#0a1533', '#1a3050', '#0a1533'],
		['#1a0f50', '#2d1a5d', '#1a0f50'],
		['#0f1a4d', '#1a2d50', '#0f1a4d'],
		['#2d0f3d', '#4d1a5d', '#2d0f3d'],
		['#0f2d3d', '#1a4d50', '#0f2d3d'],
		['#3d0f1a', '#5d1a2d', '#3d0f1a'],
		['#0f3d2d', '#1a5d4d', '#0f3d2d'],
		['#4d0f1a', '#6d1a3d', '#4d0f1a'],
		['#1a0f3d', '#3d1a5d', '#1a0f3d'],
		['#0f1a5d', '#1a3d6d', '#0f1a5d']
	];

	const backgroundAnimations = [
		'linear', // rotating linear gradient
		'radial', // pulsing radial gradient
		'spiral', // spiral pattern
		'wave', // wave interference
		'zoom' // zooming gradient
	];

	const hazardTypes = [
		{ type: 'firewall', color: '#ff0000', emoji: '🔥', glow: '#ff0000', deadly: false },
		{ type: 'antivirus', color: '#ff3333', emoji: '🦠', glow: '#ff3333', deadly: false },
		{ type: 'edr', color: '#ff6666', emoji: '⚠️', glow: '#ff6666', deadly: false },
		{ type: 'honeypot', color: '#ff9999', emoji: '🍯', glow: '#ff9999', deadly: false }
	];

	const deadlyHazardTypes = [
		{ type: 'ghost', color: '#ffffff', emoji: '👻', glow: '#ffffff', deadly: true },
		{ type: 'skeleton', color: '#ffffff', emoji: '💀', glow: '#ffffff', deadly: true }
	];

	const events = [
		{
			name: 'security scan',
			effect: () => {
				backgroundColor = '#1a0033';
				gridFlash = true;
			},
			duration: 6000,
			message: '🔍 security scan detected!'
		},
		{
			name: 'ddos attack',
			effect: () => {
				clearInterval(gameLoop);
				gameLoop = setInterval(update, 60);
			},
			duration: 7000,
			message: '💥 ddos! speed increased!'
		},
		{
			name: 'blackout',
			effect: () => {
				backgroundColor = '#000000';
			},
			duration: 6000,
			message: '🌑 blackout! low visibility!'
		},
		{
			name: 'color corruption',
			effect: () => {},
			duration: 8000,
			message: '🎨 display corrupted!'
		},
		{
			name: 'hazard spawn',
			effect: () => {
				for (let i = 0; i < 3; i++) {
					spawnHazard();
				}
			},
			duration: 0,
			message: '☠️ hazard surge!'
		}
	];

	function init() {
		if (!canvas) return;
		ctx = canvas.getContext('2d');
		canvas.width = canvasSize;
		canvas.height = canvasSize;

		loadHighScores();
	}

	function loadHighScores() {
		const saved = localStorage.getItem('snakeHighScores');
		if (saved) {
			highScores = JSON.parse(saved);
		} else {
			highScores = [];
		}
	}

	function saveHighScore(name, score) {
		highScores.push({ name: name.toUpperCase().slice(0, 3), score });
		highScores.sort((a, b) => b.score - a.score);
		highScores = highScores.slice(0, 3);
		localStorage.setItem('snakeHighScores', JSON.stringify(highScores));
	}

	function startGame() {
		snake = [{ x: 10, y: 10 }];
		direction = { x: 1, y: 0 };
		nextDirection = { x: 1, y: 0 };
		score = 0;
		hazards = [];
		hazardSpawnTimer = 0;
		backgroundColor = '#0b2063';
		currentEvent = null;
		eventTimer = 0;
		eventWarning = null;
		eventWarningTimer = 0;
		gridFlash = false;
		bonuses = [];
		bonusSpawnTimer = 0;
		activeBonus = null;
		gameOver = false;
		gameStarted = true;
		showHighScoreEntry = false;
		wallsActive = false;
		wallTimer = 0;
		backgroundColor = '#0b2063';
		backgroundGradient = null;
		backgroundAnimationFrame = 0;
		ultraMode = false;
		food = [];
		// spawn initial food items - at least one of each type
		credTypes.forEach(() => {
			spawnFood();
		});
		gameRunning = true;
		gameLoop = setInterval(update, 150);
	}

	function update() {
		if (!gameRunning) return;

		direction = nextDirection;

		const head = { x: snake[0].x + direction.x, y: snake[0].y + direction.y };

		// ultra mode - no death, gain points by moving
		if (ultraMode) {
			score += 1;
		}

		// normal collision detection (skip in ultra mode only)
		if (!ultraMode) {
			// check hazard collision (shield protects, supertrip protects from non-deadly, ghost mode protects)
			const hitHazard = hazards.find((hazard) => hazard.x === head.x && hazard.y === head.y);
			if (hitHazard) {
				// ghost mode protects from all hazards
				if (activeBonus && activeBonus.type === 'ghost') {
					// pass through, no damage
				} else if (hitHazard.deadly && activeBonus && activeBonus.type === 'supertrip') {
					// deadly hazards kill even in supertrip mode
					endGame();
					return;
				} else if (!hitHazard.deadly && activeBonus && activeBonus.type === 'supertrip') {
					// supertrip mode makes you invincible to non-deadly hazards
					// pass through, no damage
				} else if (activeBonus && activeBonus.type === 'shield') {
					activeBonus = null;
				} else {
					endGame();
					return;
				}
			}

			// check wall collision (ghost mode, supertrip, and ultra mode pass through)
			if (head.x < 0 || head.x >= tileCount || head.y < 0 || head.y >= tileCount) {
				if (
					activeBonus &&
					(activeBonus.type === 'ghost' ||
						activeBonus.type === 'supertrip' ||
						activeBonus.type === 'ultra')
				) {
					// ghost mode, supertrip, and ultra mode wrap through walls
					if (head.x < 0) head.x = tileCount - 1;
					if (head.x >= tileCount) head.x = 0;
					if (head.y < 0) head.y = tileCount - 1;
					if (head.y >= tileCount) head.y = 0;
				} else if (wallsActive) {
					if (activeBonus && activeBonus.type === 'shield') {
						// shield gives grace period
						if (!wallHitGracePeriod) {
							wallHitGracePeriod = true;
							graceTimer = 0;
							direction = { x: -direction.x, y: -direction.y };
							nextDirection = direction;
							return;
						}
					} else {
						endGame();
						return;
					}
				} else {
					// wrap around like pacman
					if (head.x < 0) head.x = tileCount - 1;
					if (head.x >= tileCount) head.x = 0;
					if (head.y < 0) head.y = tileCount - 1;
					if (head.y >= tileCount) head.y = 0;
				}
			}

			// check self collision (shield and ghost mode protect)
			if (snake.some((segment) => segment.x === head.x && segment.y === head.y)) {
				if (activeBonus && activeBonus.type === 'ghost') {
					// ghost mode passes through self
				} else if (activeBonus && activeBonus.type === 'shield') {
					activeBonus = null;
				} else {
					endGame();
					return;
				}
			}
		}

		// check bonus collision
		const bonusIndex = bonuses.findIndex((bonus) => bonus.x === head.x && bonus.y === head.y);
		if (bonusIndex !== -1) {
			activateBonus(bonuses[bonusIndex]);
			bonuses.splice(bonusIndex, 1);
		}

		snake.unshift(head);

		// magnet effect - attract nearby food
		if (activeBonus && activeBonus.type === 'magnet') {
			const magnetRadius = 2;
			food.forEach((f) => {
				const dx = head.x - f.x;
				const dy = head.y - f.y;
				const dist = Math.abs(dx) + Math.abs(dy);
				if (dist <= magnetRadius && dist > 0) {
					// move food towards snake
					if (Math.abs(dx) > Math.abs(dy)) {
						f.x += dx > 0 ? 1 : -1;
					} else {
						f.y += dy > 0 ? 1 : -1;
					}
				}
			});
		}

		// check food collision (check all food items)
		const foodIndex = food.findIndex((f) => f.x === head.x && f.y === head.y);
		if (foodIndex !== -1) {
			const eatenFood = food[foodIndex];
			const foodType = credTypes.find((c) => c.type === eatenFood.type);
			let points = foodType.points;

			// apply multipliers - ultra mode 10x, supertrip 5x, 2x regular
			if (ultraMode) {
				points = points * 10;
			} else if (activeBonus && activeBonus.type === 'supertrip') {
				points = points * 5;
			} else if (activeBonus && activeBonus.type === '2x') {
				points = points * 2;
			}

			score += points;

			// trigger background color animation
			triggerBackgroundAnimation(foodType.color);

			// remove eaten food and spawn new one
			food.splice(foodIndex, 1);
			spawnFood();

			// grow snake based on food type growth value
			// snake already has new head from unshift(head)
			// growth of 1 = don't pop (net +1), growth of 2+ = add extra segments
			for (let i = 1; i < foodType.growth; i++) {
				// duplicate the tail segment to grow
				snake.push({ ...snake[snake.length - 1] });
			}
			// don't pop tail when eating (keeps the growth)
		} else {
			// not eating food - pop tail to maintain length
			snake.pop();
		}

		// update timers
		hazardSpawnTimer++;
		colorChangeTimer++;
		eventTimer++;
		bonusSpawnTimer++;
		backgroundAnimationFrame++;

		// wall cycle
		wallTimer++;
		if (wallTimer >= wallCycleTime) {
			wallTimer = 0;
			wallsActive = !wallsActive;
		}

		// grace period countdown
		if (wallHitGracePeriod) {
			graceTimer++;
			if (graceTimer > 10) {
				wallHitGracePeriod = false;
				graceTimer = 0;
			}
		}

		// spawn hazards periodically - faster and more frequent
		if (hazardSpawnTimer > 15) {
			hazardSpawnTimer = 0;
			if (hazards.length < maxHazards && Math.random() < 0.6) {
				// only spawn deadly hazards during mushroom mode
				const inMushroomMode = activeBonus && activeBonus.type === 'supertrip';
				spawnHazard(inMushroomMode);
			}
		}

		// spawn bonuses periodically
		if (bonusSpawnTimer > 80 && bonuses.length < 2 && Math.random() < 0.3) {
			bonusSpawnTimer = 0;
			spawnBonus();
		}

		// spawn more food periodically
		if (food.length < maxFood && Math.random() < 0.02) {
			spawnFood();
		}

		// despawn old hazards
		const now = Date.now();
		hazards = hazards.filter((h) => now - h.spawnTime < hazardLifetime);

		// trigger event warning 3 seconds before event
		if (eventWarning && Date.now() - eventWarningTimer > 3000) {
			triggerRandomEvent();
			eventWarning = null;
		}

		// trigger random event warnings
		if (eventTimer > 100 && Math.random() < 0.02 && !currentEvent && !eventWarning) {
			eventTimer = 0;
			const event = events[Math.floor(Math.random() * events.length)];
			eventWarning = event;
			eventWarningTimer = Date.now();
		}

		// reset event
		if (currentEvent && Date.now() - currentEvent.startTime > currentEvent.duration) {
			resetEvent();
		}

		// expire active bonus (shield persists until hit)
		if (
			activeBonus &&
			activeBonus.type !== 'shield' &&
			activeBonus.duration > 0 &&
			Date.now() - activeBonus.startTime > activeBonus.duration
		) {
			if (activeBonus.type === 'speed') {
				clearInterval(gameLoop);
				gameLoop = setInterval(update, 150);
			}
			if (activeBonus.type === 'ultra') {
				clearInterval(gameLoop);
				gameLoop = setInterval(update, 150);
				ultraMode = false;
			}
			activeBonus = null;
		}

		// reset wall hit grace if shield lost
		if (!activeBonus || activeBonus.type !== 'shield') {
			wallHitGracePeriod = false;
			graceTimer = 0;
		}

		// fade background animation
		if (backgroundGradient && backgroundAnimationFrame > 30) {
			backgroundGradient = null;
			backgroundColor = '#0b2063';
		}

		draw();
	}

	function spawnFood() {
		let newFood;
		let attempts = 0;

		// ensure variety by preferring types that are less common on screen
		const foodCounts = {};
		credTypes.forEach((cred) => {
			foodCounts[cred.type] = food.filter((f) => f.type === cred.type).length;
		});

		// pick the least common type (or random if tied)
		const minCount = Math.min(...Object.values(foodCounts));
		const rareFoodTypes = credTypes.filter((cred) => foodCounts[cred.type] === minCount);
		const selectedType = rareFoodTypes[Math.floor(Math.random() * rareFoodTypes.length)].type;

		do {
			newFood = {
				x: Math.floor(Math.random() * tileCount),
				y: Math.floor(Math.random() * tileCount),
				type: selectedType
			};
			attempts++;

			// check collisions with snake, hazards, bonuses, and other food
			const onSnake = snake.some((segment) => segment.x === newFood.x && segment.y === newFood.y);
			const onHazard = hazards.some((hazard) => hazard.x === newFood.x && hazard.y === newFood.y);
			const onBonus = bonuses.some((bonus) => bonus.x === newFood.x && bonus.y === newFood.y);
			const onFood = food.some((f) => f.x === newFood.x && f.y === newFood.y);

			if (!onSnake && !onHazard && !onBonus && !onFood) {
				break;
			}
		} while (attempts < 100);

		if (attempts < 100) {
			food.push(newFood);
		}
	}

	function spawnHazard(spawnDeadly = false) {
		let newHazard;
		let attempts = 0;
		do {
			// choose from deadly hazards if in mushroom mode, otherwise normal hazards
			const hazardPool = spawnDeadly ? deadlyHazardTypes : hazardTypes;
			const hazardType = hazardPool[Math.floor(Math.random() * hazardPool.length)];

			newHazard = {
				x: Math.floor(Math.random() * tileCount),
				y: Math.floor(Math.random() * tileCount),
				type: hazardType.type,
				color: hazardType.color,
				emoji: hazardType.emoji,
				deadly: hazardType.deadly,
				spawnTime: Date.now()
			};
			attempts++;

			// check if position is safe (not on snake, other hazards, bonuses, or food)
			const onSnake = snake.some(
				(segment) => segment.x === newHazard.x && segment.y === newHazard.y
			);
			const onHazard = hazards.some(
				(hazard) => hazard.x === newHazard.x && hazard.y === newHazard.y
			);
			const onBonus = bonuses.some((bonus) => bonus.x === newHazard.x && bonus.y === newHazard.y);
			const onFood = food.some((f) => f.x === newHazard.x && f.y === newHazard.y);

			// check if hazard is in snake's path (same row/column as direction)
			const head = snake[0];
			const inPath =
				(direction.x !== 0 && newHazard.y === head.y) || // moving horizontally, same row
				(direction.y !== 0 && newHazard.x === head.x); // moving vertically, same column

			if (!onSnake && !onHazard && !onBonus && !onFood && !inPath) {
				break;
			}
		} while (attempts < 100);

		if (attempts < 100) {
			hazards.push(newHazard);
		}
	}

	function spawnBonus() {
		let newBonus;
		let attempts = 0;
		do {
			const bonusType = bonusTypes[Math.floor(Math.random() * bonusTypes.length)];
			newBonus = {
				x: Math.floor(Math.random() * tileCount),
				y: Math.floor(Math.random() * tileCount),
				type: bonusType.type,
				color: bonusType.color,
				emoji: bonusType.emoji,
				effect: bonusType.effect,
				duration: bonusType.duration,
				spawnTime: Date.now()
			};
			attempts++;
		} while (
			attempts < 100 &&
			(snake.some((segment) => segment.x === newBonus.x && segment.y === newBonus.y) ||
				hazards.some((hazard) => hazard.x === newBonus.x && hazard.y === newBonus.y) ||
				bonuses.some((bonus) => bonus.x === newBonus.x && bonus.y === newBonus.y) ||
				food.some((f) => f.x === newBonus.x && f.y === newBonus.y))
		);

		bonuses.push(newBonus);
	}

	function activateBonus(bonus) {
		// check if both trippy modes active = ultra mode
		if (
			bonus.type === 'trippy' &&
			activeBonus &&
			(activeBonus.type === 'trippy' || activeBonus.type === 'supertrip')
		) {
			// activate ultra mode
			ultraMode = true;
			clearInterval(gameLoop);
			gameLoop = setInterval(update, 300); // 2x slower
			activeBonus = {
				type: 'ultra',
				effect: 'ultra mode',
				duration: 8000,
				startTime: Date.now()
			};
			return;
		}

		if (bonus.type === 'supertrip' && activeBonus && activeBonus.type === 'trippy') {
			// activate ultra mode (only trippy + supertrip, not supertrip + supertrip)
			ultraMode = true;
			clearInterval(gameLoop);
			gameLoop = setInterval(update, 300); // 2x slower

			// reverse snake direction for ultra mode
			direction = { x: -direction.x, y: -direction.y };
			nextDirection = direction;

			activeBonus = {
				type: 'ultra',
				effect: 'ultra mode',
				duration: 8000,
				startTime: Date.now()
			};
			return;
		}

		// ignore eating another supertrip when already in supertrip mode
		if (bonus.type === 'supertrip' && activeBonus && activeBonus.type === 'supertrip') {
			return;
		}

		// don't override supertrip with non-trippy bonuses (only ultra can override)
		if (
			activeBonus &&
			activeBonus.type === 'supertrip' &&
			bonus.type !== 'trippy' &&
			bonus.type !== 'supertrip'
		) {
			return;
		}

		// don't override trippy with non-supertrip bonuses
		if (activeBonus && activeBonus.type === 'trippy' && bonus.type !== 'supertrip') {
			return;
		}

		activeBonus = {
			type: bonus.type,
			effect: bonus.effect,
			duration: bonus.duration,
			startTime: Date.now()
		};

		if (bonus.type === 'speed') {
			clearInterval(gameLoop);
			gameLoop = setInterval(update, 180);
		}

		// shrink snake by removing segments
		if (bonus.type === 'shrink') {
			const removeCount = Math.min(3, snake.length - 1);
			for (let i = 0; i < removeCount; i++) {
				snake.pop();
			}
			activeBonus = null; // instant effect
			return;
		}

		// spawn 3 bonuses and reverse direction when supertrip is activated
		if (bonus.type === 'supertrip') {
			// reverse snake direction in mushroom mode
			direction = { x: -direction.x, y: -direction.y };
			nextDirection = direction;

			for (let i = 0; i < 3; i++) {
				if (bonuses.length < 5) {
					spawnBonus();
				}
			}
		}
	}

	function triggerRandomEvent() {
		if (currentEvent) return;
		if (!eventWarning) return;

		// use the pre-warned event
		currentEvent = {
			...eventWarning,
			startTime: Date.now()
		};

		eventWarning.effect();
	}

	function resetEvent() {
		if (currentEvent && currentEvent.name === 'ddos attack') {
			clearInterval(gameLoop);
			gameLoop = setInterval(update, 150);
		}

		backgroundColor = '#0b2063';
		gridFlash = false;
		currentEvent = null;
	}

	function triggerBackgroundAnimation(color) {
		backgroundAnimationFrame = 0;
		// pick random varied gradient
		backgroundGradient =
			backgroundGradients[Math.floor(Math.random() * backgroundGradients.length)];
	}

	function draw() {
		if (!ctx) return;

		const superTrip = activeBonus && activeBonus.type === 'supertrip';
		// ultra mode has even wilder visuals
		if (ultraMode) {
			const time = Date.now() / 20; // smoother animation
			for (let y = 0; y < tileCount; y++) {
				for (let x = 0; x < tileCount; x++) {
					// extreme psychedelic pattern with chaotic layering
					const dist = Math.sqrt(Math.pow(x - tileCount / 2, 2) + Math.pow(y - tileCount / 2, 2));
					const angle = Math.atan2(y - tileCount / 2, x - tileCount / 2);

					// create extreme layered patterns
					const spiral = Math.sin(dist * 0.8 - time / 3 + angle * 5) * 80;
					const wave1 = Math.sin(time / 2 + x * 1.2) * 60;
					const wave2 = Math.cos(time / 3 + y * 1.2) * 60;
					const ripple = Math.sin(dist * 0.5 - time / 2) * 70;
					const diagonal = Math.sin((x + y) * 0.8 + time / 3) * 50;
					const chaos = Math.sin(time / 4 + x * y * 0.1) * 40;

					const hue =
						(time * 10 + spiral + wave1 + wave2 + ripple + diagonal + chaos + x * 15 + y * 15) %
						360;

					// extreme vivid, rapidly pulsating saturation and lightness
					const sat = 90 + Math.sin(time / 4 + x * 0.8 + y * 0.8) * 10;
					const light = 50 + Math.sin(time / 5 + dist * 0.5 + angle * 2) * 30;

					ctx.fillStyle = `hsl(${hue}, ${sat}%, ${light}%)`;
					ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);

					// add intense gradient overlays
					if ((x + y + Math.floor(time / 5)) % 2 === 0) {
						const gradient = ctx.createRadialGradient(
							x * gridSize + gridSize / 2,
							y * gridSize + gridSize / 2,
							0,
							x * gridSize + gridSize / 2,
							y * gridSize + gridSize / 2,
							gridSize * 2
						);
						const overlayHue = (hue + 90 + Math.sin(time / 8) * 90) % 360;
						gradient.addColorStop(0, `hsla(${overlayHue}, 100%, 70%, 0.7)`);
						gradient.addColorStop(0.5, `hsla(${(overlayHue + 120) % 360}, 100%, 60%, 0.5)`);
						gradient.addColorStop(1, `hsla(${(overlayHue + 240) % 360}, 100%, 50%, 0.3)`);
						ctx.fillStyle = gradient;
						ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);
					}

					// add rapidly moving streaks
					if (x % 2 === Math.floor(time / 10) % 2) {
						const streakHue = (hue + 180) % 360;
						ctx.fillStyle = `hsla(${streakHue}, 100%, 60%, 0.4)`;
						ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);
					}

					if (y % 2 === Math.floor(time / 12) % 2) {
						const streakHue = (hue + 240) % 360;
						ctx.fillStyle = `hsla(${streakHue}, 100%, 60%, 0.4)`;
						ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);
					}

					// add kaleidoscope effect
					if ((x + y) % 3 === Math.floor(time / 8) % 3) {
						const kaleido = Math.sin(dist * 0.3 + time / 6 + angle * 3) * 180;
						const kaleidoHue = (hue + kaleido) % 360;
						ctx.fillStyle = `hsla(${kaleidoHue}, 100%, 70%, 0.3)`;
						ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);
					}
				}
			}
		} else if (superTrip) {
			const time = Date.now() / 40; // smoother animation
			for (let y = 0; y < tileCount; y++) {
				for (let x = 0; x < tileCount; x++) {
					// enhanced psychedelic pattern with multiple wave functions and pattern variations
					const dist = Math.sqrt(Math.pow(x - tileCount / 2, 2) + Math.pow(y - tileCount / 2, 2));
					const angle = Math.atan2(y - tileCount / 2, x - tileCount / 2);

					// create multiple layered patterns with smoother transitions
					const spiral = Math.sin(dist * 0.4 - time / 6 + angle * 2.5) * 50;
					const wave1 = Math.sin(time / 5 + x * 0.6) * 35;
					const wave2 = Math.cos(time / 7 + y * 0.6) * 35;
					const ripple = Math.sin(dist * 0.25 - time / 4) * 45;
					const diagonal = Math.sin((x + y) * 0.4 + time / 6) * 25;

					const hue = (time * 4 + spiral + wave1 + wave2 + ripple + diagonal + x * 8 + y * 8) % 360;

					// vivid, smoothly pulsating saturation and lightness
					const sat = 85 + Math.sin(time / 10 + x * 0.4 + y * 0.4) * 15;
					const light = 45 + Math.sin(time / 12 + dist * 0.25 + angle) * 15;

					ctx.fillStyle = `hsl(${hue}, ${sat}%, ${light}%)`;
					ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);

					// add multiple gradient overlays for depth and color variety
					if ((x + y + Math.floor(time / 10)) % 2 === 0) {
						const gradient = ctx.createRadialGradient(
							x * gridSize + gridSize / 2,
							y * gridSize + gridSize / 2,
							0,
							x * gridSize + gridSize / 2,
							y * gridSize + gridSize / 2,
							gridSize * 1.5
						);
						const overlayHue = (hue + 120 + Math.sin(time / 15) * 60) % 360;
						gradient.addColorStop(0, `hsla(${overlayHue}, 100%, 60%, 0.5)`);
						gradient.addColorStop(0.5, `hsla(${(overlayHue + 60) % 360}, 100%, 50%, 0.3)`);
						gradient.addColorStop(1, `hsla(${(overlayHue + 120) % 360}, 100%, 40%, 0)`);
						ctx.fillStyle = gradient;
						ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);
					}

					// add contrasting streaks
					if (x % 3 === Math.floor(time / 20) % 3) {
						const streakHue = (hue + 180) % 360;
						ctx.fillStyle = `hsla(${streakHue}, 100%, 50%, 0.2)`;
						ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);
					}

					if (y % 3 === Math.floor(time / 25) % 3) {
						const streakHue = (hue + 240) % 360;
						ctx.fillStyle = `hsla(${streakHue}, 100%, 50%, 0.2)`;
						ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);
					}
				}
			}
		} else if (backgroundGradient) {
			// select random animation style based on frame - smoother transitions
			const animStyle =
				backgroundAnimations[
					Math.floor(backgroundAnimationFrame / 80) % backgroundAnimations.length
				];

			if (animStyle === 'radial') {
				// pulsing radial gradient from center - smoother pulse
				const pulse = Math.sin(backgroundAnimationFrame / 25) * 0.3 + 0.7;
				const gradient = ctx.createRadialGradient(
					canvasSize / 2,
					canvasSize / 2,
					0,
					canvasSize / 2,
					canvasSize / 2,
					canvasSize * pulse
				);
				gradient.addColorStop(0, backgroundGradient[0]);
				gradient.addColorStop(0.5, backgroundGradient[1]);
				gradient.addColorStop(1, backgroundGradient[2]);
				ctx.fillStyle = gradient;
				ctx.fillRect(0, 0, canvasSize, canvasSize);
			} else if (animStyle === 'spiral') {
				// spiral pattern
				for (let y = 0; y < tileCount; y++) {
					for (let x = 0; x < tileCount; x++) {
						const dx = x - tileCount / 2;
						const dy = y - tileCount / 2;
						const angle = Math.atan2(dy, dx);
						const dist = Math.sqrt(dx * dx + dy * dy);
						const spiral = (angle + dist * 0.15 - backgroundAnimationFrame / 30) % (Math.PI * 2);
						const colorIndex = Math.floor((spiral / (Math.PI * 2)) * 3);
						ctx.fillStyle = backgroundGradient[colorIndex % 3];
						ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);
					}
				}
			} else if (animStyle === 'wave') {
				// wave interference pattern
				for (let y = 0; y < tileCount; y++) {
					for (let x = 0; x < tileCount; x++) {
						const wave1 = Math.sin((x + backgroundAnimationFrame / 15) * 0.25);
						const wave2 = Math.sin((y + backgroundAnimationFrame / 15) * 0.25);
						const combined = (wave1 + wave2) / 2;
						const colorIndex = Math.floor((combined + 1) * 1.5) % 3;
						ctx.fillStyle = backgroundGradient[colorIndex];
						ctx.fillRect(x * gridSize, y * gridSize, gridSize, gridSize);
					}
				}
			} else if (animStyle === 'zoom') {
				// zooming gradient - smoother zoom
				const zoom = Math.sin(backgroundAnimationFrame / 30) * 0.5 + 1;
				const gradient = ctx.createRadialGradient(
					canvasSize / 2,
					canvasSize / 2,
					canvasSize * 0.1 * zoom,
					canvasSize / 2,
					canvasSize / 2,
					canvasSize * zoom
				);
				gradient.addColorStop(0, backgroundGradient[0]);
				gradient.addColorStop(0.5, backgroundGradient[1]);
				gradient.addColorStop(1, backgroundGradient[2]);
				ctx.fillStyle = gradient;
				ctx.fillRect(0, 0, canvasSize, canvasSize);
			} else {
				// default linear rotating gradient - smoother rotation
				const angle = (backgroundAnimationFrame * 4) % 360;
				const radians = (angle * Math.PI) / 180;
				const x1 = canvasSize / 2 + Math.cos(radians) * canvasSize;
				const y1 = canvasSize / 2 + Math.sin(radians) * canvasSize;
				const x2 = canvasSize / 2 - Math.cos(radians) * canvasSize;
				const y2 = canvasSize / 2 - Math.sin(radians) * canvasSize;

				const gradient = ctx.createLinearGradient(x1, y1, x2, y2);
				gradient.addColorStop(0, backgroundGradient[0]);
				gradient.addColorStop(0.5, backgroundGradient[1]);
				gradient.addColorStop(1, backgroundGradient[2]);
				ctx.fillStyle = gradient;
				ctx.fillRect(0, 0, canvasSize, canvasSize);
			}
		} else {
			ctx.fillStyle = backgroundColor;
			ctx.fillRect(0, 0, canvasSize, canvasSize);
		}

		// grid
		ctx.strokeStyle = gridFlash ? '#9622fcaa' : '#9622fc22';
		ctx.lineWidth = 1;
		for (let i = 0; i <= tileCount; i++) {
			ctx.beginPath();
			ctx.moveTo(i * gridSize, 0);
			ctx.lineTo(i * gridSize, canvasSize);
			ctx.stroke();
			ctx.beginPath();
			ctx.moveTo(0, i * gridSize);
			ctx.lineTo(canvasSize, i * gridSize);
			ctx.stroke();
		}

		// draw walls if active or warning
		const timeUntilSwitch = wallCycleTime - wallTimer;
		const isWarning = timeUntilSwitch <= wallWarningTime;

		if (wallsActive || isWarning) {
			ctx.lineWidth = 6;
			if (wallsActive) {
				ctx.strokeStyle = '#ff0000';
			} else {
				// warning flash
				const flashOn = Math.floor(Date.now() / 200) % 2 === 0;
				ctx.strokeStyle = flashOn ? '#ff6600' : '#ffaa00';
			}
			ctx.strokeRect(3, 3, canvasSize - 6, canvasSize - 6);
		}

		const colorCorrupted = currentEvent && currentEvent.name === 'color corruption';
		const trippyMode = activeBonus && activeBonus.type === 'trippy';
		const time = Date.now() / 100;

		// hazards with blinking effect
		const now = Date.now();
		hazards.forEach((hazard) => {
			const age = now - hazard.spawnTime;
			const timeLeft = hazardLifetime - age;
			const shouldBlink = timeLeft < 2000 && Math.floor(timeLeft / 200) % 2 === 0;

			if (!shouldBlink) {
				// deadly hazards have black background, not affected by trippy mode
				if (hazard.deadly) {
					// black background for deadly hazards
					ctx.fillStyle = '#000000';
					ctx.fillRect(hazard.x * gridSize, hazard.y * gridSize, gridSize, gridSize);

					// white outline
					ctx.strokeStyle = '#ffffff';
					ctx.lineWidth = 3;
					ctx.strokeRect(
						hazard.x * gridSize + 1,
						hazard.y * gridSize + 1,
						gridSize - 2,
						gridSize - 2
					);

					ctx.shadowBlur = 0;
					ctx.fillStyle = hazard.color;
				} else {
					// normal hazards with red outline
					ctx.strokeStyle = '#ff0000';
					ctx.lineWidth = 3;
					ctx.strokeRect(
						hazard.x * gridSize + 1,
						hazard.y * gridSize + 1,
						gridSize - 2,
						gridSize - 2
					);

					ctx.fillStyle = colorCorrupted
						? `#${Math.floor(Math.random() * 16777215).toString(16)}`
						: hazard.color;
					ctx.shadowBlur = 20;
					ctx.shadowColor = '#ff0000';
					ctx.fillRect(
						hazard.x * gridSize + 3,
						hazard.y * gridSize + 3,
						gridSize - 6,
						gridSize - 6
					);
					ctx.shadowBlur = 0;
				}

				ctx.font = `${gridSize - 6}px Arial`;
				ctx.textAlign = 'center';
				ctx.textBaseline = 'middle';
				ctx.fillText(
					hazard.emoji,
					hazard.x * gridSize + gridSize / 2,
					hazard.y * gridSize + gridSize / 2
				);
			}
		});

		// bonuses
		bonuses.forEach((bonus) => {
			ctx.strokeStyle = bonus.color;
			ctx.lineWidth = 2;
			ctx.strokeRect(bonus.x * gridSize + 1, bonus.y * gridSize + 1, gridSize - 2, gridSize - 2);

			const hue = (time + bonus.x * 10 + bonus.y * 10) % 360;
			ctx.fillStyle = trippyMode ? `hsl(${hue}, 100%, 50%)` : bonus.color;
			ctx.shadowBlur = 15;
			ctx.shadowColor = bonus.color;
			ctx.fillRect(bonus.x * gridSize + 3, bonus.y * gridSize + 3, gridSize - 6, gridSize - 6);
			ctx.shadowBlur = 0;

			ctx.font = `${gridSize - 6}px Arial`;
			ctx.textAlign = 'center';
			ctx.textBaseline = 'middle';
			ctx.fillText(
				bonus.emoji,
				bonus.x * gridSize + gridSize / 2,
				bonus.y * gridSize + gridSize / 2
			);
		});

		// snake
		snake.forEach((segment, index) => {
			// set opacity for ghost mode
			if (activeBonus && activeBonus.type === 'ghost') {
				ctx.globalAlpha = 0.5;
			}

			if (index === 0) {
				const hue = trippyMode ? (time + segment.x * 10) % 360 : null;
				ctx.fillStyle = colorCorrupted
					? `#${Math.floor(Math.random() * 16777215).toString(16)}`
					: trippyMode
						? `hsl(${hue}, 100%, 50%)`
						: '#ff00ff';

				// magnet mode - add pulsating golden glow to head
				if (activeBonus && activeBonus.type === 'magnet') {
					const magnetPulse = 20 + Math.sin(time / 3) * 10;
					ctx.shadowBlur = magnetPulse;
					ctx.shadowColor = '#ffd700';
				} else {
					ctx.shadowBlur = superTrip ? 20 : 10;
					ctx.shadowColor = trippyMode ? `hsl(${hue}, 100%, 50%)` : '#ff00ff';
				}
			} else {
				const hue = trippyMode ? (time + segment.x * 10 + segment.y * 10) % 360 : null;
				ctx.fillStyle = colorCorrupted
					? `#${Math.floor(Math.random() * 16777215).toString(16)}`
					: trippyMode
						? `hsl(${hue}, 100%, 50%)`
						: '#9622fc';
				ctx.shadowBlur = superTrip ? 15 : 5;
				ctx.shadowColor = trippyMode ? `hsl(${hue}, 100%, 50%)` : '#9622fc';
			}
			ctx.fillRect(segment.x * gridSize + 2, segment.y * gridSize + 2, gridSize - 4, gridSize - 4);

			// add pulsating outline during supertrip
			if (superTrip) {
				const pulseHue = (time * 5 + segment.x * 20 + segment.y * 20) % 360;
				const pulseWidth = 2 + Math.sin(time / 5 + index) * 1;
				ctx.strokeStyle = `hsl(${pulseHue}, 100%, 60%)`;
				ctx.lineWidth = pulseWidth;
				ctx.strokeRect(
					segment.x * gridSize + 1,
					segment.y * gridSize + 1,
					gridSize - 2,
					gridSize - 2
				);
			}

			ctx.shadowBlur = 0;

			// reset opacity after drawing segment
			if (activeBonus && activeBonus.type === 'ghost') {
				ctx.globalAlpha = 1.0;
			}

			if (index === 0 && activeBonus && activeBonus.type === 'shield') {
				ctx.strokeStyle = '#00ff00';
				ctx.lineWidth = 3;
				ctx.strokeRect(segment.x * gridSize, segment.y * gridSize, gridSize, gridSize);
			}

			// ghost mode indicator - purple outline with pulsing
			if (index === 0 && activeBonus && activeBonus.type === 'ghost') {
				const ghostPulse = 2 + Math.sin(time / 4) * 1;
				ctx.strokeStyle = '#9370db';
				ctx.lineWidth = ghostPulse;
				ctx.strokeRect(segment.x * gridSize, segment.y * gridSize, gridSize, gridSize);
			}

			// magnet mode indicator - pulsating golden outline
			if (index === 0 && activeBonus && activeBonus.type === 'magnet') {
				const magnetPulseWidth = 3 + Math.sin(time / 3) * 1.5;
				ctx.strokeStyle = '#ffd700';
				ctx.lineWidth = magnetPulseWidth;
				ctx.strokeRect(segment.x * gridSize, segment.y * gridSize, gridSize, gridSize);
			}
		});

		// draw all food items
		food.forEach((foodItem) => {
			const foodType = credTypes.find((c) => c.type === foodItem.type);
			const foodHue = trippyMode ? (time + foodItem.x * 10 + foodItem.y * 10) % 360 : null;

			// add colored glow around emoji
			ctx.shadowBlur = 20;
			ctx.shadowColor = trippyMode ? `hsl(${foodHue}, 100%, 50%)` : foodType.color;

			// draw emoji (no background rectangle)
			ctx.font = `${gridSize - 2}px Arial`;
			ctx.textAlign = 'center';
			ctx.textBaseline = 'middle';
			ctx.fillText(
				foodType.emoji,
				foodItem.x * gridSize + gridSize / 2,
				foodItem.y * gridSize + gridSize / 2
			);

			ctx.shadowBlur = 0;
		});
	}

	function endGame() {
		gameRunning = false;
		gameOver = true;
		clearInterval(gameLoop);

		if (highScores.length < 3 || score > highScores[highScores.length - 1].score) {
			showHighScoreEntry = true;
		}

		onGameOver(score);
	}

	function submitHighScore() {
		if (playerName.trim().length > 0) {
			saveHighScore(playerName, score);
			showHighScoreEntry = false;
			playerName = '';
		}
	}

	function restart() {
		startGame();
	}

	function handleKeyDown(e) {
		if (showHighScoreEntry) return;

		// allow restart with ENTER or SPACE when game is over
		if (gameOver && (e.key === 'Enter' || e.key === ' ')) {
			restart();
			e.preventDefault();
			return;
		}

		if (!gameRunning && !gameOver) return;

		switch (e.key) {
			case 'ArrowUp':
			case 'w':
			case 'W':
				if (direction.y === 0) nextDirection = { x: 0, y: -1 };
				break;
			case 'ArrowDown':
			case 's':
			case 'S':
				if (direction.y === 0) nextDirection = { x: 0, y: 1 };
				break;
			case 'ArrowLeft':
			case 'a':
			case 'A':
				if (direction.x === 0) nextDirection = { x: -1, y: 0 };
				break;
			case 'ArrowRight':
			case 'd':
			case 'D':
				if (direction.x === 0) nextDirection = { x: 1, y: 0 };
				break;
			case 'Escape':
				onClose();
				break;
		}
		e.preventDefault();
	}

	onMount(() => {
		window.addEventListener('keydown', handleKeyDown);
		init();
	});

	onDestroy(() => {
		window.removeEventListener('keydown', handleKeyDown);
		clearInterval(gameLoop);
	});
</script>

<div class="flex flex-col items-center gap-4 p-6">
	{#if gameStarted}
		<div class="flex justify-between w-full max-w-[500px] mb-2">
			<div class="text-pc-green">
				<div class="text-xs uppercase tracking-wider">score</div>
				<div class="text-2xl font-bold">{score}</div>
			</div>
			<div class="text-pc-pink">
				<div class="text-xs uppercase tracking-wider">high score</div>
				<div class="text-2xl font-bold">{highScores.length > 0 ? highScores[0].score : 0}</div>
			</div>
		</div>
	{/if}

	<div class="relative" style="width: {canvasSize}px; height: {canvasSize}px;">
		<canvas
			bind:this={canvas}
			class="border-2 border-pc-pink rounded shadow-lg shadow-pc-pink/50"
			style="display: block; width: {canvasSize}px; height: {canvasSize}px;"
		>
		</canvas>

		{#if !gameStarted}
			<div
				class="absolute inset-0 flex flex-col items-center justify-center bg-black/90 rounded backdrop-blur-sm p-6 overflow-y-auto"
			>
				<div class="text-pc-lightblue text-sm w-full max-w-[480px] mb-6 space-y-4">
					<div>
						<div class="text-pc-green font-bold uppercase text-xs mb-2">collect:</div>
						<div class="grid grid-cols-4 gap-2 text-xs">
							{#each credTypes as cred}
								<div class="flex flex-col items-center gap-1 text-center">
									<span class="text-2xl">{cred.emoji}</span>
									<span class="text-white text-xs">{cred.type}</span>
									<span style="color: {cred.color}" class="font-bold">+{cred.points}</span>
								</div>
							{/each}
						</div>
					</div>

					<div>
						<div class="text-pc-red font-bold uppercase text-xs mb-2">avoid (red outline):</div>
						<div class="grid grid-cols-4 gap-2 text-xs">
							{#each hazardTypes as hazard}
								<div class="flex flex-col items-center gap-1 text-center">
									<span class="text-2xl">{hazard.emoji}</span>
									<span class="text-white text-xs">{hazard.type}</span>
								</div>
							{/each}
						</div>
					</div>

					<div>
						<div class="text-yellow-400 font-bold uppercase text-xs mb-2">bonuses:</div>
						<div class="grid grid-cols-4 gap-2 text-xs">
							{#each bonusTypes as bonus}
								<div class="flex flex-col items-center gap-1">
									<span class="text-2xl">{bonus.emoji}</span>
									<span class="text-white text-xs">{bonus.effect}</span>
								</div>
							{/each}
						</div>
					</div>
				</div>

				<button
					on:click={startGame}
					class="px-8 py-3 bg-gradient-to-r from-pc-pink to-pc-purple text-white font-bold rounded-lg hover:shadow-lg hover:shadow-pc-pink/50 transition-all"
				>
					start game
				</button>
			</div>
		{:else if showHighScoreEntry}
			<!-- svelte-ignore a11y-click-events-have-key-events -->
			<!-- svelte-ignore a11y-no-static-element-interactions -->
			<div
				class="absolute inset-0 flex flex-col items-center justify-center bg-black/90 rounded backdrop-blur-sm p-6"
				on:click|stopPropagation
			>
				<div class="text-3xl font-bold text-pc-green mb-4">new high score!</div>
				<div class="text-2xl text-pc-pink mb-6">score: {score}</div>
				<div class="mb-6">
					<label for="player-name" class="text-pc-lightblue text-sm mb-2 block"
						>enter your name (3 letters):</label
					>
					<input
						id="player-name"
						type="text"
						maxlength="3"
						bind:value={playerName}
						on:keydown={(e) => {
							if (e.key === 'Enter') submitHighScore();
							e.stopPropagation();
						}}
						on:click|stopPropagation
						class="px-4 py-2 bg-pc-darkblue border-2 border-pc-pink rounded text-white text-center text-2xl uppercase font-bold focus:outline-none focus:border-pc-pink-hover"
						placeholder="AAA"
					/>
				</div>
				<button
					on:click={submitHighScore}
					disabled={playerName.trim().length === 0}
					class="px-8 py-3 bg-gradient-to-r from-pc-pink to-pc-purple text-white font-bold rounded-lg hover:shadow-lg hover:shadow-pc-pink/50 transition-all disabled:opacity-50 disabled:cursor-not-allowed"
				>
					submit
				</button>
			</div>
		{:else if gameOver}
			<div
				class="absolute inset-0 flex flex-col items-center justify-center bg-black/80 rounded backdrop-blur-sm"
			>
				<div class="text-4xl font-bold text-pc-red mb-4">game over!</div>
				<div class="text-2xl text-pc-pink mb-6">score: {score}</div>

				{#if highScores.length > 0}
					<div class="mb-6 w-full max-w-[300px]">
						<div class="text-pc-purple font-bold uppercase text-sm mb-2 text-center">
							top 3 scores:
						</div>
						<div class="bg-black/60 rounded border border-pc-purple/30 p-3">
							{#each highScores as hs, i}
								<div class="flex justify-between text-xs text-pc-lightblue mb-1">
									<span class="text-pc-pink">{i + 1}. {hs.name}</span>
									<span class="text-white font-bold">{hs.score}</span>
								</div>
							{/each}
						</div>
					</div>
				{/if}

				<button
					on:click={restart}
					class="px-8 py-3 bg-gradient-to-r from-pc-pink to-pc-purple text-white font-bold rounded-lg hover:shadow-lg hover:shadow-pc-pink/50 transition-all"
				>
					play again
				</button>
			</div>
		{/if}
	</div>

	{#if gameStarted}
		<div class="w-full max-w-[500px] mt-3" style="min-height: 48px;">
			{#if activeBonus}
				<div
					class="w-full p-2 bg-pc-green/20 border-2 border-pc-green rounded-lg text-center animate-pulse"
				>
					<div class="text-pc-green font-bold text-sm">
						{activeBonus.type === '2x'
							? '✨ 2X POINTS'
							: activeBonus.type === 'trippy'
								? '🌀 RAINBOW MODE'
								: activeBonus.type === 'shield'
									? wallHitGracePeriod
										? '🛡️ SHIELD - GRACE PERIOD!'
										: '🛡️ SHIELD ACTIVE'
									: activeBonus.type === 'supertrip'
										? '🍄 PSYCHEDELIC MODE'
										: activeBonus.type === 'ultra'
											? '💫 ULTRA MODE - INVINCIBLE!'
											: activeBonus.type === 'ghost'
												? '👤 GHOST MODE - PASS THROUGH!'
												: activeBonus.type === 'magnet'
													? '🧲 MAGNET - ATTRACTING FOOD!'
													: '⚡ SLOW MOTION'}
					</div>
				</div>
			{:else if eventWarning}
				<div
					class="w-full p-2 bg-yellow-500/20 border-2 border-yellow-500 rounded-lg text-center animate-pulse"
				>
					<div class="text-yellow-400 font-bold text-sm">
						⚠️ WARNING: {eventWarning.message.toUpperCase()} IN {Math.max(
							1,
							Math.ceil((3000 - (Date.now() - eventWarningTimer)) / 1000)
						)}s
					</div>
				</div>
			{:else if currentEvent}
				<div
					class="w-full p-2 bg-cta-orange/20 border-2 border-cta-orange rounded-lg text-center animate-pulse"
				>
					<div class="text-cta-orange font-bold text-sm">{currentEvent.message}</div>
				</div>
			{:else if wallCycleTime - wallTimer <= wallWarningTime && !wallsActive}
				<div
					class="w-full p-2 bg-cta-orange/20 border-2 border-cta-orange rounded-lg text-center animate-pulse"
				>
					<div class="text-cta-orange font-bold text-sm">
						⚠️ WALLS ACTIVATING IN {Math.ceil((wallCycleTime - wallTimer) / 10)}s!
					</div>
				</div>
			{/if}
		</div>
	{/if}
</div>

<style>
	@keyframes pulse {
		0%,
		100% {
			opacity: 1;
		}
		50% {
			opacity: 0.7;
		}
	}

	.animate-pulse {
		animation: pulse 1s ease-in-out infinite;
	}
</style>
