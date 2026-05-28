      ******************************************************************
      * PAYROLL.CBL — anonymized banking-style batch program fixture.  *
      * Exercises every capability the COBOL extractor delivers:       *
      *   PROGRAM-ID, four DIVISIONs, SECTIONs, PARAGRAPHs,            *
      *   PERFORM (intra), CALL '...' (inter-program), COPY (import),  *
      *   file I/O (OPEN/READ/WRITE), and EXEC SQL (embedded DB2).     *
      ******************************************************************
       IDENTIFICATION DIVISION.
       PROGRAM-ID. PAYROLL.
       AUTHOR. ANON.

       ENVIRONMENT DIVISION.
       INPUT-OUTPUT SECTION.
       FILE-CONTROL.
           SELECT EMP-FILE ASSIGN TO EMPIN
               ORGANIZATION IS SEQUENTIAL.
           SELECT PAY-FILE ASSIGN TO PAYOUT
               ORGANIZATION IS SEQUENTIAL.

       DATA DIVISION.
       FILE SECTION.
       FD  EMP-FILE.
       01  EMP-RECORD.
           05  EMP-ID            PIC X(06).
           05  EMP-NAME          PIC X(30).
           05  EMP-RATE          PIC 9(05)V99.

       WORKING-STORAGE SECTION.
       01  WS-COUNTERS.
           05  WS-EMP-COUNT      PIC 9(05) VALUE ZERO.
           05  WS-TOTAL-PAY      PIC 9(09)V99 VALUE ZERO.
       01  WS-FLAGS.
           05  WS-EOF-FLAG       PIC X VALUE 'N'.
               88  WS-EOF        VALUE 'Y'.
       COPY EMPREC.
       COPY TAXRULES.

       LINKAGE SECTION.
       01  LK-RUN-DATE           PIC X(08).

       PROCEDURE DIVISION USING LK-RUN-DATE.
       MAIN-PROCESS.
           PERFORM INIT-PROGRAM
           PERFORM PROCESS-EMPLOYEES UNTIL WS-EOF
           PERFORM FINALIZE-PROGRAM
           GOBACK.

       INIT-PROGRAM.
           OPEN INPUT EMP-FILE
           OPEN OUTPUT PAY-FILE
           MOVE ZERO TO WS-EMP-COUNT.

       PROCESS-EMPLOYEES.
           READ EMP-FILE
               AT END SET WS-EOF TO TRUE
           END-READ
           PERFORM CALCULATE-PAY
           PERFORM PERSIST-PAY
           WRITE EMP-RECORD
           ADD 1 TO WS-EMP-COUNT.

       CALCULATE-PAY.
           CALL 'TAXCALC' USING EMP-RATE WS-TOTAL-PAY
           COMPUTE WS-TOTAL-PAY = EMP-RATE * 40.

       PERSIST-PAY.
           EXEC SQL
               INSERT INTO PAYROLL_LEDGER
                   (EMP_ID, AMOUNT)
               VALUES (:EMP-ID, :WS-TOTAL-PAY)
           END-EXEC
           EXEC SQL
               SELECT DEPT_CODE INTO :WS-DEPT
                   FROM EMPLOYEE
                   WHERE EMP_ID = :EMP-ID
           END-EXEC.

       FINALIZE-PROGRAM.
           CALL 'AUDITLOG' USING WS-EMP-COUNT
           CLOSE EMP-FILE
           CLOSE PAY-FILE.
