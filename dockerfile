FROM rockylinux:9

# RUN dnf update -y && dnf install -y openldap-clients sssd sssd-ldap oddjob-mkhomedir golang
RUN dnf update -y && dnf install -y golang

# COPY ldap.conf /etc/openldap/ldap.conf
# COPY sssd.conf /etc/sssd/sssd.conf
# COPY service/main.go /main.go
COPY startup /startup


# RUN chmod 0600 /etc/sssd/sssd.conf
# RUN chmod +x /startup

# CMD ["go", "run", "hello1.go"]
# CMD ["/usr/sbin/sssd", "-i"]

CMD ["./startup"]