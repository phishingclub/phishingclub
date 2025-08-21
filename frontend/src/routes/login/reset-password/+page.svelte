<script>
	import { API } from '$lib/api/api.js';
	import { goto } from '$app/navigation';
	import { onMount } from 'svelte';
	import { UserService } from '$lib/service/user';
	import FormButton from '$lib/components/FormButton.svelte';
	import Form from '$lib/components/Form.svelte';
	import SubHeadline from '$lib/components/SubHeadline.svelte';
	import { addToast } from '$lib/store/toast';
	import FormError from '$lib/components/FormError.svelte';
	import PasswordField from '$lib/components/PasswordField.svelte';
	import HeadTitle from '$lib/components/HeadTitle.svelte';

	// services
	const api = API.instance;
	const userService = UserService.instance;

	// bindings
	const changePasswordFormValues = {
		currentPassword: null,
		newPassword: null,
		repeatNewPassword: null
	};

	// local state
	let changePasswordError = '';

	// component logic

	// hooks
	onMount(() => {
		console.log('no implemented');
		location.href = '/login';
	});

	// component logic
	const onSubmitChangePassword = async () => {
		changePasswordError = '';
		// check if the new password and repeated password match
		if (changePasswordFormValues.newPassword !== changePasswordFormValues.repeatNewPassword) {
			changePasswordError = 'Current and repeated password do not match.';
			return;
		}
		try {
			const res = await api.user.changePassword(
				changePasswordFormValues.currentPassword,
				changePasswordFormValues.newPassword
			);
			if (!res.success) {
				changePasswordError = res.error;
				return;
			}
			addToast('Password changed - login required', 'Success');
			userService.clear();
			console.info('profile: changed password - navigating to login');
			goto('/login/');
		} catch (e) {
			addToast('Failed to change password', 'Error');
			console.error('failed to change password', e);
		}
	};
</script>

<HeadTitle title="Change password" />
<main>
	<Form on:submit={onSubmitChangePassword}>
		<SubHeadline>Current password has expired. Set a new password</SubHeadline>
		<PasswordField bind:value={changePasswordFormValues.currentPassword}
			>Current password</PasswordField
		>
		<PasswordField bind:value={changePasswordFormValues.newPassword}>New password</PasswordField>
		<PasswordField bind:value={changePasswordFormValues.repeatNewPassword}
			>Repeat new password</PasswordField
		>
		<FormError message={changePasswordError} />
		<FormButton>Change Password</FormButton>
	</Form>
</main>
