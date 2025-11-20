<script>
	import { onMount } from 'svelte';

	export let onGameOver = () => {};
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

	const canvasWidth = 400;
	const canvasHeight = 600;
	const lureSize = 20;
	const maxDepthValue = 10000;

	// game state
	let lure = { x: canvasWidth / 2, y: 50, speed: 3 };
	let descending = true;
	let reachedBottom = false;
	let bottomPauseTimer = 0;
	let catchPauseTimer = 0;
	let employees = [];
	let traps = [];
	let caughtEmployees = []; // each has: employee data, offsetX, offsetY, angle
	let depth = 0;
	let combo = 0;

	// employee types with different point values
	const employeeTypes = [
		{
			type: 'intern',
			color: '#4ade80',
			points: 10,
			emoji: '🧑‍💻',
			title: 'intern',
			minDepth: 0,
			size: 30
		},
		{
			type: 'employee',
			color: '#60a5fa',
			points: 25,
			emoji: '👔',
			title: 'employee',
			minDepth: 0,
			size: 35
		},
		{
			type: 'analyst',
			color: '#22d3ee',
			points: 40,
			emoji: '📊',
			title: 'analyst',
			minDepth: 1000,
			size: 38
		},
		{
			type: 'manager',
			color: '#a78bfa',
			points: 75,
			emoji: '👨‍💼',
			title: 'manager',
			minDepth: 2000,
			size: 42
		},
		{
			type: 'senior',
			color: '#c084fc',
			points: 120,
			emoji: '🎓',
			title: 'senior',
			minDepth: 3500,
			size: 45
		},
		{
			type: 'director',
			color: '#f472b6',
			points: 200,
			emoji: '🎯',
			title: 'director',
			minDepth: 5000,
			size: 48
		},
		{
			type: 'vp',
			color: '#fb923c',
			points: 350,
			emoji: '💼',
			title: 'vp',
			minDepth: 6500,
			size: 52
		},
		{
			type: 'cto',
			color: '#fbbf24',
			points: 500,
			emoji: '⚡',
			title: 'cto',
			minDepth: 7500,
			size: 55
		},
		{
			type: 'cfo',
			color: '#facc15',
			points: 750,
			emoji: '💰',
			title: 'cfo',
			minDepth: 8500,
			size: 58
		},
		{
			type: 'ceo',
			color: '#fde047',
			points: 1000,
			emoji: '👑',
			title: 'ceo',
			minDepth: 9500,
			size: 62,
			hasRing: true
		}
	];

	// trap/obstacle types
	const trapTypes = [
		{ type: 'firewall', color: '#ef4444', emoji: '🔥', title: 'firewall', size: 55 },
		{ type: 'antivirus', color: '#f59e0b', emoji: '🛡️', title: 'antivirus', size: 48 },
		{ type: 'training', color: '#ec4899', emoji: '📚', title: 'security training', size: 52 },
		{ type: 'passkey', color: '#8b5cf6', emoji: '🔐', title: 'passkey', size: 45 }
	];

	function init() {
		if (canvas) {
			ctx = canvas.getContext('2d');
			canvas.width = canvasWidth;
			canvas.height = canvasHeight;
		}
		loadHighScores();
	}

	function loadHighScores() {
		try {
			const saved = localStorage.getItem('phishingGameHighScores');
			if (saved) {
				highScores = JSON.parse(saved);
			}
		} catch (e) {
			console.error('failed to load high scores:', e);
		}
	}

	function saveHighScore(name) {
		highScores.push({ name, score: Math.floor(score) });
		highScores.sort((a, b) => b.score - a.score);
		highScores = highScores.slice(0, 3);
		localStorage.setItem('phishingGameHighScores', JSON.stringify(highScores));
	}

	function startGame() {
		score = 0;
		gameStarted = true;
		gameRunning = true;
		gameOver = false;
		descending = true;
		reachedBottom = false;
		bottomPauseTimer = 0;
		catchPauseTimer = 0;
		depth = 0;
		combo = 0;
		lure = { x: canvasWidth / 2, y: 50, speed: 3 };
		employees = [];
		traps = [];
		caughtEmployees = [];

		// spawn initial obstacles
		spawnInitialObstacles();

		gameLoop = setInterval(() => {
			update();
			draw();
		}, 1000 / 60);
	}

	function spawnInitialObstacles() {
		// spawn employees at various depths
		for (let i = 0; i < 40; i++) {
			spawnEmployee(Math.random() * maxDepthValue + 200);
		}

		// spawn traps at various depths
		for (let i = 0; i < 25; i++) {
			spawnTrap(Math.random() * maxDepthValue + 300);
		}
	}

	function spawnEmployee(atDepth) {
		// filter employee types available at this depth
		const availableTypes = employeeTypes.filter((type) => atDepth >= type.minDepth);

		if (availableTypes.length === 0) return;

		// weight higher value employees less
		const weights = availableTypes.map((_, i) => Math.pow(0.6, i));
		const totalWeight = weights.reduce((a, b) => a + b, 0);
		const rand = Math.random() * totalWeight;

		let cumulative = 0;
		let selectedType = availableTypes[0];

		for (let i = 0; i < availableTypes.length; i++) {
			cumulative += weights[i];
			if (rand <= cumulative) {
				selectedType = availableTypes[i];
				break;
			}
		}

		employees.push({
			x: Math.random() * (canvasWidth - selectedType.size) + selectedType.size / 2,
			y: atDepth,
			...selectedType,
			vx: (Math.random() - 0.5) * 2,
			direction: Math.random() > 0.5 ? 1 : -1
		});
	}

	function spawnTrap(atDepth) {
		const trapType = trapTypes[Math.floor(Math.random() * trapTypes.length)];
		traps.push({
			x: Math.random() * (canvasWidth - trapType.size) + trapType.size / 2,
			y: atDepth,
			...trapType
		});
	}

	function update() {
		if (!gameRunning) return;

		if (descending) {
			// move lure down
			lure.y += lure.speed;
			depth = lure.y;

			// check if reached bottom (10000m)
			if (lure.y >= maxDepthValue) {
				lure.y = maxDepthValue;
				reachedBottom = true;
				bottomPauseTimer = 60; // pause for 1 second (60 frames)
				descending = false;
			}

			// check collision with employees (catch and start ascending)
			for (let i = employees.length - 1; i >= 0; i--) {
				const emp = employees[i];
				if (checkCollision(lure, emp, emp.size)) {
					// catch employee and start ascending
					score += emp.points;
					combo++;
					caughtEmployees.push({
						...emp,
						offsetX: (Math.random() - 0.5) * 30,
						offsetY: (Math.random() - 0.5) * 30 + caughtEmployees.length * 15,
						angle: (Math.random() - 0.5) * 60
					});
					employees.splice(i, 1);
					descending = false;
					catchPauseTimer = 60; // pause for 1 second before ascending
					break;
				}
			}

			// check collision with traps (instant death)
			for (const trap of traps) {
				if (checkCollision(lure, trap, trap.size)) {
					endGame();
					return;
				}
			}

			// move employees
			for (const emp of employees) {
				emp.x += emp.vx * emp.direction;
				if (emp.x < emp.size / 2 || emp.x > canvasWidth - emp.size / 2) {
					emp.direction *= -1;
				}
			}

			// spawn more obstacles as we go deeper
			if (Math.random() < 0.008) {
				spawnEmployee(lure.y + canvasHeight);
			}
			if (Math.random() < 0.005) {
				spawnTrap(lure.y + canvasHeight);
			}
		} else {
			// handle bottom pause
			if (bottomPauseTimer > 0) {
				bottomPauseTimer--;
				if (bottomPauseTimer === 0) {
					lure.speed = 4;
				}
				// don't move during pause
			} else if (catchPauseTimer > 0) {
				// handle catch pause
				catchPauseTimer--;
				if (catchPauseTimer === 0) {
					lure.speed = 4;
				}
				// don't move during pause
			} else {
				// ascending - move lure up
				lure.y -= lure.speed;
			}

			// check if reached surface
			if (lure.y < 0) {
				endGame();
				return;
			}

			// check collision with employees (catch them while ascending)
			for (let i = employees.length - 1; i >= 0; i--) {
				const emp = employees[i];
				if (checkCollision(lure, emp, emp.size)) {
					// catch employee
					score += emp.points * (1 + combo * 0.1);
					combo++;
					caughtEmployees.push({
						...emp,
						offsetX: (Math.random() - 0.5) * 30,
						offsetY: (Math.random() - 0.5) * 30 + caughtEmployees.length * 15,
						angle: (Math.random() - 0.5) * 60
					});
					employees.splice(i, 1);
				}
			}

			// check collision with traps (instant death - lose all points)
			for (const trap of traps) {
				if (checkCollision(lure, trap, trap.size)) {
					score = 0;
					endGame();
					return;
				}
			}

			// move employees
			for (const emp of employees) {
				emp.x += emp.vx * emp.direction;
				if (emp.x < emp.size / 2 || emp.x > canvasWidth - emp.size / 2) {
					emp.direction *= -1;
				}
			}
		}

		// handle keyboard input for horizontal movement
		if (keys.left && lure.x > lureSize) {
			lure.x -= 3;
		}
		if (keys.right && lure.x < canvasWidth - lureSize) {
			lure.x += 3;
		}
	}

	function checkCollision(obj1, obj2, size) {
		const dx = obj1.x - obj2.x;
		const dy = obj1.y - obj2.y;
		const distance = Math.sqrt(dx * dx + dy * dy);
		return distance < size / 2 + lureSize / 2;
	}

	function draw() {
		if (!ctx) return;

		// clear canvas with depth-based darkness (much darker)
		const depthRatio = Math.min(depth / maxDepthValue, 1);
		const darkness = Math.floor(10 * (1 - depthRatio * 0.95)); // 10 to almost 0
		ctx.fillStyle = `rgb(${darkness}, ${darkness}, ${darkness})`;
		ctx.fillRect(0, 0, canvasWidth, canvasHeight);

		// draw depth gradient that gets darker and more intense deeper
		const gradient = ctx.createLinearGradient(0, 0, 0, canvasHeight);
		const alpha1 = 0.05 * (1 - depthRatio * 0.9);
		const alpha2 = 0.15 * (1 - depthRatio * 0.85);
		const alpha3 = 0.25 * (1 - depthRatio * 0.8);
		gradient.addColorStop(0, `rgba(14, 165, 233, ${alpha1})`);
		gradient.addColorStop(0.5, `rgba(14, 165, 233, ${alpha2})`);
		gradient.addColorStop(1, `rgba(8, 47, 73, ${alpha3})`);
		ctx.fillStyle = gradient;
		ctx.fillRect(0, 0, canvasWidth, canvasHeight);

		// calculate camera offset (follow lure)
		let cameraY = 0;
		if (lure.y > canvasHeight / 2 && descending) {
			cameraY = lure.y - canvasHeight / 2;
		} else if (!descending && lure.y < depth - canvasHeight / 2) {
			cameraY = Math.max(0, lure.y - canvasHeight / 2);
		} else if (!descending) {
			cameraY = Math.max(0, depth - canvasHeight);
		}

		ctx.save();
		ctx.translate(0, -cameraY);

		// draw the bottom floor
		ctx.fillStyle = '#1a1a2e';
		ctx.fillRect(0, maxDepthValue, canvasWidth, 100);

		// draw bottom glow
		const bottomGradient = ctx.createLinearGradient(0, maxDepthValue - 50, 0, maxDepthValue);
		bottomGradient.addColorStop(0, 'rgba(139, 92, 246, 0)');
		bottomGradient.addColorStop(1, 'rgba(139, 92, 246, 0.5)');
		ctx.fillStyle = bottomGradient;
		ctx.fillRect(0, maxDepthValue - 50, canvasWidth, 50);

		// draw bottom text
		ctx.fillStyle = '#8b5cf6';
		ctx.font = 'bold 24px monospace';
		ctx.textAlign = 'center';
		ctx.fillText('BOTTOM', canvasWidth / 2, maxDepthValue + 30);
		ctx.font = '14px monospace';
		ctx.fillText('10,000m', canvasWidth / 2, maxDepthValue + 55);

		// draw depth markers
		ctx.strokeStyle = 'rgba(139, 92, 246, 0.2)';
		ctx.lineWidth = 1;
		for (let i = 0; i < maxDepthValue + canvasHeight; i += 100) {
			ctx.beginPath();
			ctx.moveTo(0, i);
			ctx.lineTo(canvasWidth, i);
			ctx.stroke();

			ctx.fillStyle = 'rgba(139, 92, 246, 0.3)';
			ctx.font = '10px monospace';
			ctx.fillText(`${i}m`, 5, i - 5);
		}

		// draw traps
		for (const trap of traps) {
			if (trap.y > cameraY - trap.size && trap.y < cameraY + canvasHeight + trap.size) {
				// glow (stronger deeper)
				const trapDepthRatio = Math.min(trap.y / maxDepthValue, 1);
				ctx.shadowBlur = 20 + trapDepthRatio * 30;
				ctx.shadowColor = trap.color;

				// draw trap
				ctx.fillStyle = trap.color;
				ctx.fillRect(trap.x - trap.size / 2, trap.y - trap.size / 2, trap.size, trap.size);

				// emoji
				ctx.shadowBlur = 0;
				ctx.font = `${trap.size * 0.6}px Arial`;
				ctx.textAlign = 'center';
				ctx.textBaseline = 'middle';
				ctx.fillText(trap.emoji, trap.x, trap.y);
			}
		}

		// draw employees
		for (const emp of employees) {
			if (emp.y > cameraY - emp.size && emp.y < cameraY + canvasHeight + emp.size) {
				// glow (stronger deeper)
				const empDepthRatio = Math.min(emp.y / maxDepthValue, 1);
				ctx.shadowBlur = 15 + empDepthRatio * 25;
				ctx.shadowColor = emp.color;

				// draw glowing ring for CEO
				if (emp.hasRing) {
					ctx.strokeStyle = emp.color;
					ctx.lineWidth = 3;
					ctx.shadowBlur = 30 + empDepthRatio * 40;
					ctx.beginPath();
					ctx.arc(emp.x, emp.y, emp.size / 2 + 8, 0, Math.PI * 2);
					ctx.stroke();
				}

				// draw employee circle
				ctx.fillStyle = emp.color;
				ctx.beginPath();
				ctx.arc(emp.x, emp.y, emp.size / 2, 0, Math.PI * 2);
				ctx.fill();

				// emoji
				ctx.shadowBlur = 0;
				ctx.font = `${emp.size * 0.6}px Arial`;
				ctx.textAlign = 'center';
				ctx.textBaseline = 'middle';
				ctx.fillText(emp.emoji, emp.x, emp.y);
			}
		}

		// draw fishing line
		ctx.strokeStyle = 'rgba(255, 255, 255, 0.5)';
		ctx.lineWidth = 2;
		ctx.beginPath();
		ctx.moveTo(lure.x, 0);
		ctx.lineTo(lure.x, lure.y);
		ctx.stroke();

		// draw caught employees attached to hook
		for (const caught of caughtEmployees) {
			const catchX = lure.x + caught.offsetX;
			const catchY = lure.y + caught.offsetY;

			ctx.save();
			ctx.translate(catchX, catchY);
			ctx.rotate((caught.angle * Math.PI) / 180);

			// glow (based on depth)
			const caughtDepthRatio = Math.min(lure.y / maxDepthValue, 1);
			ctx.shadowBlur = 15 + caughtDepthRatio * 25;
			ctx.shadowColor = caught.color;

			// draw glowing ring for CEO
			if (caught.hasRing) {
				ctx.strokeStyle = caught.color;
				ctx.lineWidth = 3;
				ctx.shadowBlur = 30 + caughtDepthRatio * 40;
				ctx.beginPath();
				ctx.arc(0, 0, caught.size / 2 + 8, 0, Math.PI * 2);
				ctx.stroke();
				ctx.shadowBlur = 15 + caughtDepthRatio * 25;
			}

			// draw employee circle
			ctx.fillStyle = caught.color;
			ctx.beginPath();
			ctx.arc(0, 0, caught.size / 2, 0, Math.PI * 2);
			ctx.fill();

			// emoji
			ctx.shadowBlur = 0;
			ctx.font = `${caught.size * 0.6}px Arial`;
			ctx.textAlign = 'center';
			ctx.textBaseline = 'middle';
			ctx.fillText(caught.emoji, 0, 0);

			ctx.restore();
		}

		// draw lure (phishing email)
		const lureDepthRatio = Math.min(lure.y / maxDepthValue, 1);
		ctx.shadowBlur = 20 + lureDepthRatio * 30;
		ctx.shadowColor = descending ? '#22d3ee' : '#f59e0b';
		ctx.fillStyle = descending ? '#22d3ee' : '#f59e0b';
		ctx.beginPath();
		ctx.arc(lure.x, lure.y, lureSize / 2, 0, Math.PI * 2);
		ctx.fill();

		// draw hook
		ctx.shadowBlur = 0;
		ctx.font = `${lureSize}px Arial`;
		ctx.textAlign = 'center';
		ctx.textBaseline = 'middle';
		ctx.fillText('📧', lure.x, lure.y);

		ctx.restore();

		// draw caught employees indicator
		if (caughtEmployees.length > 0) {
			ctx.fillStyle = 'rgba(0, 0, 0, 0.7)';
			ctx.fillRect(10, canvasHeight - 60, 200, 50);

			ctx.fillStyle = '#4ade80';
			ctx.font = '14px monospace';
			ctx.fillText(`caught: ${caughtEmployees.length}`, 20, canvasHeight - 40);
			ctx.fillText(`combo: x${combo.toFixed(1)}`, 20, canvasHeight - 20);
		}

		// draw status
		ctx.fillStyle = 'rgba(0, 0, 0, 0.7)';
		ctx.fillRect(canvasWidth - 150, 10, 140, 60);

		ctx.fillStyle = '#22d3ee';
		ctx.font = '12px monospace';
		ctx.fillText(`depth: ${Math.floor(depth)}m`, canvasWidth - 140, 30);
		ctx.fillStyle = descending
			? '#4ade80'
			: bottomPauseTimer > 0 || catchPauseTimer > 0
				? '#fbbf24'
				: '#f59e0b';
		const status = descending
			? '⬇ descending'
			: bottomPauseTimer > 0
				? '⏸ bottom!'
				: catchPauseTimer > 0
					? '⏸ caught!'
					: '⬆ ascending';
		ctx.fillText(status, canvasWidth - 140, 50);
	}

	function endGame() {
		gameRunning = false;
		gameOver = true;
		clearInterval(gameLoop);

		// check if high score (top 3)
		if (highScores.length < 3 || score > highScores[highScores.length - 1].score) {
			showHighScoreEntry = true;
		}

		onGameOver();
	}

	function submitHighScore() {
		if (playerName.trim().length > 0) {
			saveHighScore(playerName.trim());
			showHighScoreEntry = false;
			playerName = '';
		}
	}

	function restart() {
		startGame();
	}

	// keyboard controls
	let keys = { left: false, right: false };

	function handleKeyDown(e) {
		if (e.key === 'Escape') {
			onClose();
			return;
		}

		if (!gameStarted) {
			if (e.key === 'Enter' || e.key === ' ') {
				e.preventDefault();
				startGame();
			}
			return;
		}

		if (gameOver && showHighScoreEntry && e.key === 'Enter') {
			e.preventDefault();
			submitHighScore();
			return;
		}

		if (gameOver && !showHighScoreEntry && (e.key === 'Enter' || e.key === ' ')) {
			e.preventDefault();
			restart();
			return;
		}

		if (e.key === 'ArrowLeft' || e.key === 'a' || e.key === 'A') {
			keys.left = true;
		}
		if (e.key === 'ArrowRight' || e.key === 'd' || e.key === 'D') {
			keys.right = true;
		}
	}

	function handleKeyUp(e) {
		if (e.key === 'ArrowLeft' || e.key === 'a' || e.key === 'A') {
			keys.left = false;
		}
		if (e.key === 'ArrowRight' || e.key === 'd' || e.key === 'D') {
			keys.right = false;
		}
	}

	onMount(() => {
		init();
		window.addEventListener('keydown', handleKeyDown);
		window.addEventListener('keyup', handleKeyUp);

		return () => {
			if (gameLoop) clearInterval(gameLoop);
			window.removeEventListener('keydown', handleKeyDown);
			window.removeEventListener('keyup', handleKeyUp);
		};
	});
</script>

<div class="flex flex-col items-center gap-4 p-6">
	{#if gameStarted}
		<div class="flex justify-between w-full max-w-[400px] mb-2">
			<div class="text-pc-green">
				<div class="text-xs uppercase tracking-wider">score</div>
				<div class="text-2xl font-bold">{Math.floor(score)}</div>
			</div>
			<div class="text-pc-pink">
				<div class="text-xs uppercase tracking-wider">depth</div>
				<div class="text-2xl font-bold">{Math.floor(depth)}m</div>
			</div>
		</div>
	{/if}

	<div class="relative" style="width: {canvasWidth}px; height: {canvasHeight}px;">
		<canvas
			bind:this={canvas}
			class="border-2 border-pc-pink rounded shadow-lg shadow-pc-pink/30"
			style="image-rendering: pixelated;"
		/>

		{#if !gameStarted}
			<div
				class="absolute inset-0 flex flex-col items-center justify-center bg-black/80 backdrop-blur-sm"
			>
				<div class="text-pc-lightblue text-sm w-full max-w-[380px] mb-6 space-y-4 px-4">
					<div>
						<div class="text-pc-green font-bold uppercase text-xs mb-2">🎣 how to play</div>
						<ul class="text-xs space-y-1 list-disc list-inside">
							<li>use ← → (or a/d) to move horizontally</li>
							<li>
								<span class="text-pc-purple">descending:</span> catch an employee to start ascending
							</li>
							<li>hitting traps = instant death!</li>
							<li>the deeper you go, the better targets appear</li>
							<li>catch multiple employees on the way up for combos</li>
							<li>reach max depth (10,000m) for the best catches!</li>
						</ul>
					</div>

					<div>
						<div class="text-pc-pink font-bold uppercase text-xs mb-2">
							🎯 targets (deeper = better)
						</div>
						<div class="grid grid-cols-5 gap-1 text-xs">
							{#each employeeTypes as emp}
								<div class="flex flex-col items-center gap-0.5 text-center">
									<span class="text-xl">{emp.emoji}</span>
									<span class="text-white text-[10px]">{emp.title}</span>
									<span style="color: {emp.color}" class="font-bold text-[10px]"
										>{emp.points}pts</span
									>
								</div>
							{/each}
						</div>
					</div>

					<div>
						<div class="text-pc-red font-bold uppercase text-xs mb-2">
							⚠️ obstacles (instant death!)
						</div>
						<div class="grid grid-cols-4 gap-2 text-xs">
							{#each trapTypes as trap}
								<div class="flex flex-col items-center gap-1 text-center">
									<span class="text-2xl">{trap.emoji}</span>
									<span class="text-white text-xs">{trap.title}</span>
								</div>
							{/each}
						</div>
					</div>
				</div>

				<button
					on:click={startGame}
					class="bg-gradient-to-r from-pc-pink to-pc-purple text-white px-8 py-3 rounded font-bold uppercase text-sm hover:shadow-lg hover:shadow-pc-pink/50 transition-all"
				>
					start phishing
				</button>
			</div>
		{:else if showHighScoreEntry}
			<div
				class="absolute inset-0 flex flex-col items-center justify-center bg-black/90 backdrop-blur-sm p-6"
			>
				<div class="text-3xl font-bold text-pc-green mb-4">new high score! 🎉</div>
				<div class="text-2xl text-pc-pink mb-6">{Math.floor(score)} points</div>

				<div class="mb-6">
					<label for="player-name" class="text-pc-lightblue text-sm mb-2 block"
						>enter your handle:</label
					>
					<input
						id="player-name"
						type="text"
						bind:value={playerName}
						on:keydown={(e) => e.key === 'Enter' && submitHighScore()}
						maxlength="20"
						class="bg-black/60 border-2 border-pc-purple text-white px-4 py-2 rounded focus:outline-none focus:border-pc-pink w-64 text-center"
						placeholder="elite_phisher"
					/>
				</div>

				<button
					on:click={submitHighScore}
					class="bg-gradient-to-r from-pc-pink to-pc-purple text-white px-8 py-3 rounded font-bold uppercase text-sm hover:shadow-lg hover:shadow-pc-pink/50 transition-all"
				>
					submit score
				</button>
			</div>
		{:else if gameOver}
			<div
				class="absolute inset-0 flex flex-col items-center justify-center bg-black/90 backdrop-blur-sm"
			>
				<div class="text-4xl font-bold text-pc-red mb-4">phishing complete! 🎣</div>
				<div class="text-2xl text-pc-pink mb-6">{Math.floor(score)} points</div>

				{#if highScores.length > 0}
					<div class="mb-6 w-full max-w-[300px]">
						<div class="text-pc-purple font-bold uppercase text-sm mb-2 text-center">
							🏆 high scores
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
					class="bg-gradient-to-r from-pc-pink to-pc-purple text-white px-8 py-3 rounded font-bold uppercase text-sm hover:shadow-lg hover:shadow-pc-pink/50 transition-all"
				>
					phish again
				</button>
			</div>
		{/if}
	</div>

	{#if gameStarted && !gameOver}
		<div class="text-pc-lightblue text-xs text-center">
			use ← → or a/d to steer • {descending
				? 'catch an employee to start ascending! avoid traps!'
				: 'catch more targets!'}
		</div>
	{/if}
</div>
