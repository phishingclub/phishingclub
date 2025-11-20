<script>
	import { onMount, onDestroy } from 'svelte';
	import { fade, fly } from 'svelte/transition';
	import SnakeGame from './SnakeGame.svelte';
	import PhishingGame from './PhishingGame.svelte';

	// konami code sequence
	const konamiCode = [
		'ArrowUp',
		'ArrowUp',
		'ArrowDown',
		'ArrowDown',
		'ArrowLeft',
		'ArrowRight',
		'ArrowLeft',
		'ArrowRight'
	];
	let konamiIndex = 0;

	// track ctrl+shift+p shortcut
	let ctrlPressed = false;
	let shiftPressed = false;

	let gameVisible = false;
	let currentGame = null;

	const games = [
		{
			name: 'snake',
			title: 'slop cred snake',
			component: SnakeGame
		},
		{
			name: 'phishing',
			title: 'slopedition',
			component: PhishingGame
		}
	];

	function getRandomGame() {
		return games[Math.floor(Math.random() * games.length)];
	}

	function handleKeyDown(e) {
		// track modifier keys
		if (e.key === 'Control') {
			ctrlPressed = true;
		}
		if (e.key === 'Shift') {
			shiftPressed = true;
		}

		// check for ctrl+shift+p to launch random game
		if (ctrlPressed && shiftPressed && (e.key === 'p' || e.key === 'P')) {
			e.preventDefault();
			currentGame = getRandomGame();
			gameVisible = true;
			return;
		}

		// check for konami code to launch random game
		if (e.key === konamiCode[konamiIndex]) {
			konamiIndex++;
			if (konamiIndex === konamiCode.length) {
				currentGame = getRandomGame();
				gameVisible = true;
				konamiIndex = 0;
			}
		} else {
			konamiIndex = 0;
		}
	}

	function handleKeyUp(e) {
		if (e.key === 'Control') {
			ctrlPressed = false;
		}
		if (e.key === 'Shift') {
			shiftPressed = false;
		}
	}

	function closeGame() {
		gameVisible = false;
	}

	function handleGameOver(score) {
		// could do something with the score here
	}

	onMount(() => {
		window.addEventListener('keydown', handleKeyDown);
		window.addEventListener('keyup', handleKeyUp);
	});

	onDestroy(() => {
		window.removeEventListener('keydown', handleKeyDown);
		window.removeEventListener('keyup', handleKeyUp);
	});
</script>

{#if gameVisible && currentGame}
	<!-- svelte-ignore a11y-click-events-have-key-events -->
	<!-- svelte-ignore a11y-no-static-element-interactions -->
	<div
		class="fixed inset-0 z-[9999] flex items-center justify-center bg-black/90 backdrop-blur-sm"
		transition:fade={{ duration: 200 }}
		on:click={closeGame}
	>
		<!-- svelte-ignore a11y-click-events-have-key-events -->
		<!-- svelte-ignore a11y-no-static-element-interactions -->
		<div
			class="relative bg-gradient-to-br from-pc-darkblue via-half-devil-gray to-black border-2 border-pc-pink shadow-2xl shadow-pc-pink/50 rounded-lg overflow-hidden"
			transition:fly={{ y: 50, duration: 300 }}
			on:click|stopPropagation
		>
			<!-- cyberpunk grid background -->
			<div class="absolute inset-0 opacity-10 pointer-events-none">
				<div
					class="absolute inset-0"
					style="background-image: repeating-linear-gradient(0deg, #ff00ff 0px, transparent 1px, transparent 20px), repeating-linear-gradient(90deg, #ff00ff 0px, transparent 1px, transparent 20px);"
				></div>
			</div>

			<!-- close button -->
			<button
				on:click={closeGame}
				class="absolute top-4 right-4 z-10 text-pc-pink hover:text-white transition-colors text-2xl font-bold w-10 h-10 flex items-center justify-center border border-pc-pink hover:border-pc-pink-hover rounded"
			>
				✕
			</button>

			<!-- header -->
			<div class="relative p-4 border-b-2 border-pc-pink/30 bg-black/40">
				<h1
					class="text-3xl font-bold text-transparent bg-clip-text bg-gradient-to-r from-pc-pink via-pc-purple to-cta-blue font-phudu text-center"
				>
					{currentGame.title}
				</h1>
				<p class="text-pc-lightblue text-xs text-center mt-1 font-titilium">press esc to close</p>
			</div>

			<!-- game content -->
			<div class="relative">
				<svelte:component
					this={currentGame.component}
					onGameOver={handleGameOver}
					onClose={closeGame}
				/>
			</div>
		</div>
	</div>
{/if}

<style>
	:global(.font-phudu) {
		font-family: 'Phudu', sans-serif;
	}
	:global(.font-titilium) {
		font-family: 'Titillium Web', sans-serif;
	}
</style>
