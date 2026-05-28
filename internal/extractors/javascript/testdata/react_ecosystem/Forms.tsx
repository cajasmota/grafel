// Forms.tsx — proving fixture for issue #2894 PR3 React Ecosystem group:
// form_library_extraction. React Hook Form (useForm/register/Controller +
// resolver schema linkage) and Formik (useFormik/<Formik>/<Field>) idioms.
// The hook calls and JSX already surface via the generic passes (USES_HOOK /
// JSX renders); decorateForms stamps the form-specific decoration.
import React from 'react';
import { useForm, Controller, useFieldArray, useFormContext } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { yupResolver } from '@hookform/resolvers/yup';
import { Formik, Form, Field, FieldArray, useFormik } from 'formik';
import { z } from 'zod';
import * as Yup from 'yup';

const loginSchema = z.object({ email: z.string(), password: z.string() });
const signupSchema = Yup.object({ name: Yup.string().required() });

// --- React Hook Form component with zod resolver (form_library_extraction) ---
export function LoginForm() {
  const { register, handleSubmit, control } = useForm({
    resolver: zodResolver(loginSchema),
  });
  return (
    <form onSubmit={handleSubmit(() => {})}>
      <input {...register('email')} />
      <input {...register('password')} />
      <Controller name="remember" control={control} render={() => <input />} />
    </form>
  );
}

// --- React Hook Form custom hook with yup resolver + field array ---
export function useProfileForm() {
  const form = useForm({ resolver: yupResolver(signupSchema) });
  useFieldArray({ control: form.control, name: 'addresses' });
  return form;
}

// --- nested field consumer via context (form_library_extraction) ---
export function AddressFields() {
  const { register } = useFormContext();
  return <input {...register('city')} />;
}

// --- Formik render-prop component with validationSchema (form_library_extraction) ---
export function SignupForm() {
  return (
    <Formik
      initialValues={{ name: '', email: '' }}
      validationSchema={signupSchema}
      onSubmit={() => {}}
    >
      <Form>
        <Field name="name" />
        <Field name="email" type="email" />
        <FieldArray name="phones" />
      </Form>
    </Formik>
  );
}

// --- Formik hook-style form (form_library_extraction) ---
export function ContactForm() {
  const formik = useFormik({
    initialValues: { message: '' },
    validationSchema: contactSchema,
    onSubmit: () => {},
  });
  return <form onSubmit={formik.handleSubmit} />;
}

const contactSchema = Yup.object({ message: Yup.string() });
